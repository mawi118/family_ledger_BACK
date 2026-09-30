package auth_test

import (
	"context"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mawi118/family_ledger_BACK/internal/auth"
	"github.com/mawi118/family_ledger_BACK/internal/db"
	"github.com/mawi118/family_ledger_BACK/internal/interceptor"
	"github.com/mawi118/family_ledger_BACK/proto"
)

const (
	bufSize    = 1024 * 1024
	testSecret = "test-secret"
	testPass   = "Testpass123!"
	credsError = "Неверное имя пользователя или пароль"
)

func setupServer(t *testing.T) proto.AuthClient {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — пропускаем интеграционный тест")
	}

	pool, err := db.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(pool.Close)

	// чистим таблицы перед каждым тестом, чтобы не зависеть от порядка запуска
	// ВНИМАНИЕ: TEST_DATABASE_URL должен указывать на отдельную тестовую БД — тут стираются пользователи.
	if _, err := pool.Exec(context.Background(), "TRUNCATE refresh_tokens, users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptor.ValidationInterceptor))
	proto.RegisterAuthServer(grpcServer, auth.NewServer(pool, []byte(testSecret), 15*time.Minute, 7*24*time.Hour))

	go func() {
		_ = grpcServer.Serve(lis)
	}()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return proto.NewAuthClient(conn)
}

func mustRegister(t *testing.T, c proto.AuthClient, email string) *proto.RegisterResponse {
	t.Helper()
	resp, err := c.Register(context.Background(), &proto.RegisterRequest{
		Email: email, Password: testPass, FirstName: "Тест",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return resp
}

func mustLogin(t *testing.T, c proto.AuthClient, email string) *proto.LoginResponse {
	t.Helper()
	resp, err := c.Login(context.Background(), &proto.LoginRequest{Email: email, Password: testPass})
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	return resp
}

func wantCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("%s: ожидали %v, получили %v", what, want, err)
	}
}

func TestRegisterLoginRefreshLogout(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	regResp := mustRegister(t, client, "test-refresh@example.com")
	if regResp.AccessToken == "" || regResp.RefreshToken == "" {
		t.Fatal("register не вернул оба токена")
	}

	// повторная регистрация тем же email — AlreadyExists с текстом из требований
	_, err := client.Register(ctx, &proto.RegisterRequest{
		Email: "test-refresh@example.com", Password: testPass, FirstName: "Тест",
	})
	wantCode(t, err, codes.AlreadyExists, "повторная регистрация")
	if status.Convert(err).Message() != "Почта уже занята" {
		t.Fatalf("неожиданный текст: %q", status.Convert(err).Message())
	}

	loginResp := mustLogin(t, client, "test-refresh@example.com")

	_, err = client.Login(ctx, &proto.LoginRequest{Email: "test-refresh@example.com", Password: "wrong-Password1!"})
	wantCode(t, err, codes.Unauthenticated, "неверный пароль")

	refreshResp, err := client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: loginResp.RefreshToken})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshResp.AccessToken == "" || refreshResp.RefreshToken == "" {
		t.Fatal("refresh не вернул оба токена")
	}

	// старый refresh уже отозван ротацией
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: loginResp.RefreshToken})
	wantCode(t, err, codes.Unauthenticated, "повтор старого refresh")

	if _, err = client.Logout(ctx, &proto.LogoutRequest{RefreshToken: refreshResp.RefreshToken}); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// после logout этот refresh больше не должен работать
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: refreshResp.RefreshToken})
	wantCode(t, err, codes.Unauthenticated, "refresh после logout")
}

func TestMe(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	regResp := mustRegister(t, client, "test-me@example.com")

	meResp, err := client.Me(ctx, &proto.MeRequest{AccessToken: regResp.AccessToken})
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if meResp.User.Email != "test-me@example.com" || meResp.User.FirstName != "Тест" {
		t.Fatalf("неожиданные данные пользователя: %+v", meResp.User)
	}

	_, err = client.Me(ctx, &proto.MeRequest{AccessToken: "garbage-token"})
	wantCode(t, err, codes.Unauthenticated, "мусорный токен")
}

// Док 3: у пользователя не может быть двух валидных токенов одновременно.
func TestSingleSession(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	first := mustRegister(t, client, "single@example.com")
	second := mustLogin(t, client, "single@example.com")

	// пара, выданная при регистрации, после нового логина мертва целиком
	_, err := client.Me(ctx, &proto.MeRequest{AccessToken: first.AccessToken})
	wantCode(t, err, codes.Unauthenticated, "старый access после нового логина")
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: first.RefreshToken})
	wantCode(t, err, codes.Unauthenticated, "старый refresh после нового логина")

	if _, err = client.Me(ctx, &proto.MeRequest{AccessToken: second.AccessToken}); err != nil {
		t.Fatalf("новый access должен работать: %v", err)
	}
}

// Док 3: logout отзывает все токены, в том числе access.
func TestLogoutRevokesAccess(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	login := func() *proto.LoginResponse {
		mustRegister(t, client, "logout@example.com")
		return mustLogin(t, client, "logout@example.com")
	}()

	if _, err := client.Me(ctx, &proto.MeRequest{AccessToken: login.AccessToken}); err != nil {
		t.Fatalf("me до logout: %v", err)
	}
	if _, err := client.Logout(ctx, &proto.LogoutRequest{RefreshToken: login.RefreshToken}); err != nil {
		t.Fatalf("logout: %v", err)
	}
	_, err := client.Me(ctx, &proto.MeRequest{AccessToken: login.AccessToken})
	wantCode(t, err, codes.Unauthenticated, "access после logout")
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: login.RefreshToken})
	wantCode(t, err, codes.Unauthenticated, "refresh после logout")
}

func TestLogoutIsIdempotent(t *testing.T) {
	client := setupServer(t)
	resp, err := client.Logout(context.Background(), &proto.LogoutRequest{RefreshToken: "unknown-token"})
	if err != nil || !resp.Success {
		t.Fatalf("logout с неизвестным токеном должен быть успешным: %v %v", resp, err)
	}
}

// Док 3: при обновлении токенов предыдущий access инвалидируется.
func TestRefreshRevokesPreviousAccess(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	reg := mustRegister(t, client, "rotate@example.com")
	refreshed, err := client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: reg.RefreshToken})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	_, err = client.Me(ctx, &proto.MeRequest{AccessToken: reg.AccessToken})
	wantCode(t, err, codes.Unauthenticated, "старый access после refresh")
	if _, err = client.Me(ctx, &proto.MeRequest{AccessToken: refreshed.AccessToken}); err != nil {
		t.Fatalf("новый access должен работать: %v", err)
	}
}

// Из N параллельных Refresh с одним токеном должен пройти ровно один.
func TestRefreshConcurrentSingleUse(t *testing.T) {
	client := setupServer(t)
	reg := mustRegister(t, client, "race@example.com")

	const n = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.Refresh(context.Background(), &proto.RefreshRequest{RefreshToken: reg.RefreshToken})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				success++
			case status.Code(err) != codes.Unauthenticated:
				t.Errorf("неожиданная ошибка: %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("ожидали ровно 1 успешный refresh, получили %d", success)
	}
}

// После N параллельных логинов должна остаться ровно одна живая сессия.
func TestConcurrentLoginsLeaveOneSession(t *testing.T) {
	client := setupServer(t)
	mustRegister(t, client, "parallel@example.com")

	const n = 8
	tokens := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := client.Login(context.Background(), &proto.LoginRequest{Email: "parallel@example.com", Password: testPass})
			if err != nil {
				t.Errorf("login: %v", err)
				return
			}
			tokens[i] = resp.RefreshToken
		}(i)
	}
	wg.Wait()

	alive := 0
	for _, rt := range tokens {
		if rt == "" {
			continue
		}
		if _, err := client.Refresh(context.Background(), &proto.RefreshRequest{RefreshToken: rt}); err == nil {
			alive++
		}
	}
	if alive != 1 {
		t.Fatalf("ожидали ровно 1 живую сессию, получили %d", alive)
	}
}

func TestEmailCaseInsensitive(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	if _, err := client.Register(ctx, &proto.RegisterRequest{
		Email: "  Case@Example.COM ", Password: testPass, FirstName: "Тест",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := client.Login(ctx, &proto.LoginRequest{Email: "CASE@example.com", Password: testPass}); err != nil {
		t.Fatalf("login с другим регистром: %v", err)
	}

	_, err := client.Register(ctx, &proto.RegisterRequest{
		Email: "case@example.com", Password: testPass, FirstName: "Тест",
	})
	wantCode(t, err, codes.AlreadyExists, "дубль почты в другом регистре")

	exists, err := client.EmailExists(ctx, &proto.EmailExistsRequest{Email: "CASE@EXAMPLE.COM"})
	if err != nil || !exists.Exists {
		t.Fatalf("EmailExists должен находить почту в любом регистре: %v %v", exists, err)
	}
}

func TestFirstNameNormalizationAndValidation(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	reg, err := client.Register(ctx, &proto.RegisterRequest{
		Email: "name@example.com", Password: testPass, FirstName: "  иван ",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	me, err := client.Me(ctx, &proto.MeRequest{AccessToken: reg.AccessToken})
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if me.User.FirstName != "Иван" {
		t.Fatalf("ожидали нормализованное имя %q, получили %q", "Иван", me.User.FirstName)
	}

	bad := map[string]string{
		"   ":   "Имя должно быть длиной от 2 символов",
		"А":     "Имя должно быть длиной от 2 символов",
		"--":    "Имя должно содержать только буквы, пробел и дефис",
		"Ив1":   "Имя должно содержать только буквы, пробел и дефис",
		"Иван-": "Имя должно содержать только буквы, пробел и дефис",
	}
	for name, wantMsg := range bad {
		_, err := client.Register(ctx, &proto.RegisterRequest{
			Email: "bad-name@example.com", Password: testPass, FirstName: name,
		})
		wantCode(t, err, codes.InvalidArgument, "имя "+name)
		if got := status.Convert(err).Message(); got != wantMsg {
			t.Errorf("имя %q: ожидали %q, получили %q", name, wantMsg, got)
		}
	}
}

// Док 3: любая неудачная авторизация — одна и та же ошибка.
func TestLoginFailuresIndistinguishable(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()
	mustRegister(t, client, "known@example.com")

	_, errWrongPass := client.Login(ctx, &proto.LoginRequest{Email: "known@example.com", Password: "Wrong-pass9!"})
	_, errNoUser := client.Login(ctx, &proto.LoginRequest{Email: "nobody@example.com", Password: testPass})

	for _, err := range []error{errWrongPass, errNoUser} {
		wantCode(t, err, codes.Unauthenticated, "неуспешный логин")
		if got := status.Convert(err).Message(); got != credsError {
			t.Fatalf("ожидали %q, получили %q", credsError, got)
		}
	}
}

func TestLoginRejectsBadEmailFormat(t *testing.T) {
	client := setupServer(t)
	_, err := client.Login(context.Background(), &proto.LoginRequest{Email: "not-an-email", Password: testPass})
	wantCode(t, err, codes.InvalidArgument, "формат почты при логине")
}

// Access без привязки к сессии или с другим алгоритмом подписи не принимается.
func TestMeRejectsForgedTokens(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()
	reg := mustRegister(t, client, "forged@example.com")

	orig := &jwt.RegisteredClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(reg.AccessToken, orig); err != nil {
		t.Fatalf("parse: %v", err)
	}

	// валидная подпись HS256, но нет jti (как у токенов старого формата)
	noJTI, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: orig.Subject, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}).SignedString([]byte(testSecret))
	_, err := client.Me(ctx, &proto.MeRequest{AccessToken: noJTI})
	wantCode(t, err, codes.Unauthenticated, "токен без jti")

	// тот же sub/jti, тот же секрет, но HS512
	hs512, _ := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.RegisteredClaims{
		Subject: orig.Subject, ID: orig.ID, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}).SignedString([]byte(testSecret))
	_, err = client.Me(ctx, &proto.MeRequest{AccessToken: hs512})
	wantCode(t, err, codes.Unauthenticated, "токен с алгоритмом HS512")

	// без exp
	noExp, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: orig.Subject, ID: orig.ID,
	}).SignedString([]byte(testSecret))
	_, err = client.Me(ctx, &proto.MeRequest{AccessToken: noExp})
	wantCode(t, err, codes.Unauthenticated, "токен без exp")
}
