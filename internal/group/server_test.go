package group_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/mawi118/family_ledger_BACK/internal/auth"
	"github.com/mawi118/family_ledger_BACK/internal/db"
	"github.com/mawi118/family_ledger_BACK/internal/group"
	"github.com/mawi118/family_ledger_BACK/internal/interceptor"
	"github.com/mawi118/family_ledger_BACK/proto"
)

const (
	testSecret = "test-secret"
	testPass   = "Testpass123!"
)

type env struct {
	pool   *pgxpool.Pool
	auth   proto.AuthClient
	groups proto.GroupsClient
}

// ВНИМАНИЕ: тесты стирают данные. TEST_DATABASE_URL должен указывать на отдельную тестовую БД,
// а пакеты тестов нужно запускать последовательно: go test ./... -p 1
func setup(t *testing.T) *env {
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
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE group_invites, group_members, groups, refresh_tokens, users CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		interceptor.NewAuthInterceptor(pool, []byte(testSecret)),
		interceptor.ValidationInterceptor,
	))
	proto.RegisterAuthServer(srv, auth.NewServer(pool, []byte(testSecret), 15*time.Minute, 7*24*time.Hour))
	proto.RegisterGroupsServer(srv, group.NewServer(pool))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &env{pool: pool, auth: proto.NewAuthClient(conn), groups: proto.NewGroupsClient(conn)}
}

type user struct {
	id, access, refresh string
}

func (e *env) newUser(t *testing.T, email string) user {
	t.Helper()
	r, err := e.auth.Register(context.Background(), &proto.RegisterRequest{Email: email, Password: testPass, FirstName: "Тест"})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return user{id: r.User.UserId, access: r.AccessToken, refresh: r.RefreshToken}
}

func as(u user) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+u.access)
}

func wantCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if got := status.Code(err); got != want {
		t.Fatalf("код ошибки: хотели %v, получили %v (%v)", want, got, err)
	}
}

func (e *env) mustGroup(t *testing.T, u user, name string) *proto.Group {
	t.Helper()
	g, err := e.groups.CreateGroup(as(u), &proto.CreateGroupRequest{Name: name})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	return g
}

// join добавляет пользователя в группу через настоящий поток: код от создателя → ввод кода.
func (e *env) join(t *testing.T, owner, u user, groupID string) {
	t.Helper()
	inv, err := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: groupID})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := e.groups.AcceptInvite(as(u), &proto.AcceptInviteRequest{Code: inv.Code}); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
}

func TestGroupsRequireAuth(t *testing.T) {
	e := setup(t)
	u := e.newUser(t, "a@example.com")

	_, err := e.groups.CreateGroup(context.Background(), &proto.CreateGroupRequest{})
	wantCode(t, err, codes.Unauthenticated)

	bad := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer not.a.jwt")
	_, err = e.groups.ListMyGroups(bad, &proto.ListMyGroupsRequest{})
	wantCode(t, err, codes.Unauthenticated)

	if _, err := e.groups.ListMyGroups(as(u), &proto.ListMyGroupsRequest{}); err != nil {
		t.Fatalf("валидный токен должен проходить: %v", err)
	}

	// после logout прежний access перестаёт работать
	if _, err := e.auth.Logout(context.Background(), &proto.LogoutRequest{RefreshToken: u.refresh}); err != nil {
		t.Fatal(err)
	}
	_, err = e.groups.ListMyGroups(as(u), &proto.ListMyGroupsRequest{})
	wantCode(t, err, codes.Unauthenticated)
}

func TestCreateGroupDefaultAndExplicitName(t *testing.T) {
	e := setup(t)
	u := e.newUser(t, "a@example.com")

	g1 := e.mustGroup(t, u, "")
	if g1.Name != "Группа 1" || g1.CreatorId != u.id {
		t.Fatalf("g1 = %+v", g1)
	}
	g2 := e.mustGroup(t, u, "  Семья  ")
	if g2.Name != "Семья" {
		t.Fatalf("имя не обрезано: %q", g2.Name)
	}
	g3 := e.mustGroup(t, u, "")
	if g3.Name != "Группа 3" {
		t.Fatalf("g3 = %q, ожидали «Группа 3»", g3.Name)
	}

	_, err := e.groups.CreateGroup(as(u), &proto.CreateGroupRequest{Name: strings.Repeat("я", 101)})
	wantCode(t, err, codes.InvalidArgument)

	list, err := e.groups.ListMyGroups(as(u), &proto.ListMyGroupsRequest{})
	if err != nil || len(list.Groups) != 3 {
		t.Fatalf("список групп: %v, %v", list, err)
	}
}

func TestGroupVisibility(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	stranger := e.newUser(t, "b@example.com")
	g := e.mustGroup(t, owner, "Семья")

	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := e.groups.GetGroup(as(stranger), &proto.GroupIdRequest{GroupId: g.GroupId})
			return err
		},
		"members": func() error {
			_, err := e.groups.ListMembers(as(stranger), &proto.GroupIdRequest{GroupId: g.GroupId})
			return err
		},
		"income": func() error {
			_, err := e.groups.GetMyIncome(as(stranger), &proto.GroupIdRequest{GroupId: g.GroupId})
			return err
		},
		"set income": func() error {
			_, err := e.groups.SetMyIncome(as(stranger), &proto.SetMyIncomeRequest{GroupId: g.GroupId, Income: 1})
			return err
		},
		"rename": func() error {
			_, err := e.groups.RenameGroup(as(stranger), &proto.RenameGroupRequest{GroupId: g.GroupId, Name: "x"})
			return err
		},
		"invite": func() error {
			_, err := e.groups.CreateInvite(as(stranger), &proto.GroupIdRequest{GroupId: g.GroupId})
			return err
		},
		"bad uuid": func() error {
			_, err := e.groups.GetGroup(as(stranger), &proto.GroupIdRequest{GroupId: "nope"})
			return err
		},
		"unknown": func() error {
			_, err := e.groups.GetGroup(as(stranger), &proto.GroupIdRequest{GroupId: "00000000-0000-0000-0000-000000000000"})
			return err
		},
	} {
		if err := call(); status.Code(err) != codes.NotFound {
			t.Errorf("%s: ожидали NotFound, получили %v", name, err)
		}
	}
}

func TestRenameOnlyOwner(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	member := e.newUser(t, "b@example.com")
	g := e.mustGroup(t, owner, "Семья")
	e.join(t, owner, member, g.GroupId)

	_, err := e.groups.RenameGroup(as(member), &proto.RenameGroupRequest{GroupId: g.GroupId, Name: "Хак"})
	wantCode(t, err, codes.PermissionDenied)

	_, err = e.groups.RenameGroup(as(owner), &proto.RenameGroupRequest{GroupId: g.GroupId, Name: "   "})
	wantCode(t, err, codes.InvalidArgument)

	r, err := e.groups.RenameGroup(as(owner), &proto.RenameGroupRequest{GroupId: g.GroupId, Name: " Наша семья "})
	if err != nil || r.Name != "Наша семья" {
		t.Fatalf("rename: %v, %v", r, err)
	}
	got, _ := e.groups.GetGroup(as(member), &proto.GroupIdRequest{GroupId: g.GroupId})
	if got.Name != "Наша семья" {
		t.Fatalf("имя не сохранилось: %q", got.Name)
	}
}

func TestInviteFlow(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	bob := e.newUser(t, "b@example.com")
	carol := e.newUser(t, "c@example.com")
	g := e.mustGroup(t, owner, "Семья")

	inv1, err := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if err != nil || len(inv1.Code) != 4 {
		t.Fatalf("invite: %v, %v", inv1, err)
	}
	exp, err := time.Parse(time.RFC3339, inv1.ExpiresAt)
	if err != nil || time.Until(exp) > 5*time.Minute+5*time.Second || time.Until(exp) < 4*time.Minute {
		t.Fatalf("expiresAt = %q (%v)", inv1.ExpiresAt, err)
	}
	// пока код жив, возвращается он же
	inv2, _ := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if inv2.Code != inv1.Code || inv2.ExpiresAt != inv1.ExpiresAt {
		t.Fatalf("ожидали тот же код: %v vs %v", inv1, inv2)
	}

	// создатель уже в группе: 409, и код при этом не сгорает
	_, err = e.groups.AcceptInvite(as(owner), &proto.AcceptInviteRequest{Code: inv1.Code})
	wantCode(t, err, codes.AlreadyExists)

	acc, err := e.groups.AcceptInvite(as(bob), &proto.AcceptInviteRequest{Code: inv1.Code})
	if err != nil || acc.GroupId != g.GroupId {
		t.Fatalf("accept: %v, %v", acc, err)
	}

	// использованный код = истёкший
	_, err = e.groups.AcceptInvite(as(carol), &proto.AcceptInviteRequest{Code: inv1.Code})
	wantCode(t, err, codes.NotFound)

	// обычный участник приглашать не может
	_, err = e.groups.CreateInvite(as(bob), &proto.GroupIdRequest{GroupId: g.GroupId})
	wantCode(t, err, codes.PermissionDenied)

	// а создатель сразу получает новый код
	inv3, err := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.groups.AcceptInvite(as(carol), &proto.AcceptInviteRequest{Code: inv3.Code}); err != nil {
		t.Fatalf("новый код должен работать: %v", err)
	}

	_, err = e.groups.AcceptInvite(as(carol), &proto.AcceptInviteRequest{Code: "12a4"})
	wantCode(t, err, codes.InvalidArgument)
}

func TestExpiredInvite(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	bob := e.newUser(t, "b@example.com")
	g := e.mustGroup(t, owner, "Семья")

	inv, _ := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE group_invites SET expires_at = now() - interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	_, err := e.groups.AcceptInvite(as(bob), &proto.AcceptInviteRequest{Code: inv.Code})
	wantCode(t, err, codes.NotFound)

	inv2, err := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if err != nil {
		t.Fatalf("после истечения должен выдаваться новый код: %v", err)
	}
	if _, err := e.groups.AcceptInvite(as(bob), &proto.AcceptInviteRequest{Code: inv2.Code}); err != nil {
		t.Fatalf("accept нового кода: %v", err)
	}
}

func TestInviteCodesUniqueAcrossGroups(t *testing.T) {
	e := setup(t)
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		o := e.newUser(t, fmt.Sprintf("o%d@example.com", i))
		g := e.mustGroup(t, o, "")
		inv, err := e.groups.CreateInvite(as(o), &proto.GroupIdRequest{GroupId: g.GroupId})
		if err != nil {
			t.Fatal(err)
		}
		if seen[inv.Code] {
			t.Fatalf("код %s выдан двум живым приглашениям", inv.Code)
		}
		seen[inv.Code] = true
	}
}

func TestInviteAttemptLimit(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	bob := e.newUser(t, "b@example.com")
	g := e.mustGroup(t, owner, "Семья")
	inv, _ := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})

	wrong := "0000"
	if inv.Code == wrong {
		wrong = "0001"
	}
	for i := 0; i < 5; i++ {
		_, err := e.groups.AcceptInvite(as(bob), &proto.AcceptInviteRequest{Code: wrong})
		wantCode(t, err, codes.NotFound)
	}
	// шестая попытка блокируется, даже если код верный
	_, err := e.groups.AcceptInvite(as(bob), &proto.AcceptInviteRequest{Code: inv.Code})
	wantCode(t, err, codes.ResourceExhausted)

	// лимит персональный: другой пользователь не затронут
	carol := e.newUser(t, "c@example.com")
	if _, err := e.groups.AcceptInvite(as(carol), &proto.AcceptInviteRequest{Code: inv.Code}); err != nil {
		t.Fatalf("другой пользователь не должен блокироваться: %v", err)
	}
}

func TestConcurrentAcceptSingleUse(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	g := e.mustGroup(t, owner, "Семья")
	inv, _ := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})

	const n = 8
	users := make([]user, n)
	for i := range users {
		users[i] = e.newUser(t, fmt.Sprintf("u%d@example.com", i))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, u := range users {
		wg.Add(1)
		go func(u user) {
			defer wg.Done()
			if _, err := e.groups.AcceptInvite(as(u), &proto.AcceptInviteRequest{Code: inv.Code}); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}(u)
	}
	wg.Wait()
	if ok != 1 {
		t.Fatalf("одноразовый код сработал %d раз, ожидали ровно 1", ok)
	}
}

func TestMemberLimit(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	g := e.mustGroup(t, owner, "Семья")
	for i := 0; i < 9; i++ { // создатель + 9 = 10
		e.join(t, owner, e.newUser(t, fmt.Sprintf("m%d@example.com", i)), g.GroupId)
	}
	extra := e.newUser(t, "extra@example.com")
	inv, _ := e.groups.CreateInvite(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	_, err := e.groups.AcceptInvite(as(extra), &proto.AcceptInviteRequest{Code: inv.Code})
	wantCode(t, err, codes.FailedPrecondition)

	// код при отказе не сгорел
	var left int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM group_invites`).Scan(&left); err != nil || left != 1 {
		t.Fatalf("код должен остаться: count=%d err=%v", left, err)
	}
}

func TestMembersOrderAndIncome(t *testing.T) {
	e := setup(t)
	owner := e.newUser(t, "a@example.com")
	bob := e.newUser(t, "b@example.com")
	carol := e.newUser(t, "c@example.com")
	g := e.mustGroup(t, owner, "Семья")
	e.join(t, owner, bob, g.GroupId)
	time.Sleep(10 * time.Millisecond)
	e.join(t, owner, carol, g.GroupId)

	// доход по умолчанию 0
	inc, err := e.groups.GetMyIncome(as(bob), &proto.GroupIdRequest{GroupId: g.GroupId})
	if err != nil || inc.Income != 0 {
		t.Fatalf("income: %v, %v", inc, err)
	}
	if _, err := e.groups.SetMyIncome(as(bob), &proto.SetMyIncomeRequest{GroupId: g.GroupId, Income: 5000000}); err != nil {
		t.Fatal(err)
	}
	_, err = e.groups.SetMyIncome(as(bob), &proto.SetMyIncomeRequest{GroupId: g.GroupId, Income: -1})
	wantCode(t, err, codes.InvalidArgument)

	// чужой доход не меняется
	oc, _ := e.groups.GetMyIncome(as(owner), &proto.GroupIdRequest{GroupId: g.GroupId})
	if oc.Income != 0 {
		t.Fatalf("доход создателя изменился: %d", oc.Income)
	}

	// порядок: сам запрашивающий, затем создатель, затем по дате вступления
	m, err := e.groups.ListMembers(as(carol), &proto.GroupIdRequest{GroupId: g.GroupId})
	if err != nil || len(m.Members) != 3 {
		t.Fatalf("members: %v, %v", m, err)
	}
	order := []string{m.Members[0].UserId, m.Members[1].UserId, m.Members[2].UserId}
	want := []string{carol.id, owner.id, bob.id}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("порядок участников: %v, ожидали %v", order, want)
		}
	}
	if m.Members[1].Role != "owner" || m.Members[2].Income != 5000000 || m.Members[0].JoinedAt == "" {
		t.Fatalf("поля участников: %+v", m.Members)
	}
}
