package auth_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

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

const bufSize = 1024 * 1024

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
	if _, err := pool.Exec(context.Background(), "TRUNCATE refresh_tokens, users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	lis := bufconn.Listen(bufSize)
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(interceptor.ValidationInterceptor))
	proto.RegisterAuthServer(grpcServer, auth.NewServer(pool, []byte("test-secret"), 15*time.Minute, 30*24*time.Hour))

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

func TestRegisterLoginRefreshLogout(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	regResp, err := client.Register(ctx, &proto.RegisterRequest{
		Email:     "test-refresh@example.com",
		Password:  "Testpass123!",
		FirstName: "Тест",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if regResp.AccessToken == "" || regResp.RefreshToken == "" {
		t.Fatal("register не вернул оба токена")
	}

	// повторная регистрация тем же email — должна дать AlreadyExists
	_, err = client.Register(ctx, &proto.RegisterRequest{
		Email: "test-refresh@example.com", Password: "Testpass123!", FirstName: "Тест",
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("ожидали AlreadyExists, получили %v", err)
	}

	loginResp, err := client.Login(ctx, &proto.LoginRequest{
		Email: "test-refresh@example.com", Password: "Testpass123!",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	_, err = client.Login(ctx, &proto.LoginRequest{
		Email: "test-refresh@example.com", Password: "wrong-password",
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ожидали Unauthenticated на неверный пароль, получили %v", err)
	}

	refreshResp, err := client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: loginResp.RefreshToken})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshResp.AccessToken == "" || refreshResp.RefreshToken == "" {
		t.Fatal("refresh не вернул оба токена")
	}

	// старый refresh уже отозван ротацией — повторное использование должно упасть
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: loginResp.RefreshToken})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ожидали, что старый refresh отклонят, получили %v", err)
	}

	_, err = client.Logout(ctx, &proto.LogoutRequest{RefreshToken: refreshResp.RefreshToken})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	// после logout этот refresh больше не должен работать
	_, err = client.Refresh(ctx, &proto.RefreshRequest{RefreshToken: refreshResp.RefreshToken})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ожидали, что отозванный refresh отклонят, получили %v", err)
	}
}

func TestMe(t *testing.T) {
	client := setupServer(t)
	ctx := context.Background()

	regResp, err := client.Register(ctx, &proto.RegisterRequest{
		Email:     "test-me@example.com",
		Password:  "Testpass123!",
		FirstName: "Тест",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	meResp, err := client.Me(ctx, &proto.MeRequest{AccessToken: regResp.AccessToken})
	if err != nil {
		t.Fatalf("me: %v", err)
	}
	if meResp.User.Email != "test-me@example.com" || meResp.User.FirstName != "Тест" {
		t.Fatalf("неожиданные данные пользователя: %+v", meResp.User)
	}

	_, err = client.Me(ctx, &proto.MeRequest{AccessToken: "garbage-token"})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ожидали Unauthenticated на мусорный токен, получили %v", err)
	}
}
