package group

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/internal/interceptor"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxMembers      = 10
	inviteTTL       = 5 * time.Minute
	inviteCodeTries = 20
	maxFailedCodes  = 5
	failedCodesWin  = 5 * time.Minute

	roleOwner  = "owner"
	roleMember = "member"
)

var (
	errUnauthorized = status.Error(codes.Unauthenticated, "Не авторизован")
	errInternal     = status.Error(codes.Internal, "internal error")
	// Несуществующая группа и группа, где пользователь не состоит, неотличимы: не раскрываем чужие id.
	errGroupNotFound = status.Error(codes.NotFound, "Группа не найдена")
	errForbidden     = status.Error(codes.PermissionDenied, "Недостаточно прав")
	errCodeNotFound  = status.Error(codes.NotFound, "Код не найден или истёк")
	errAlreadyMember = status.Error(codes.AlreadyExists, "Вы уже состоите в этой группе")
	errGroupFull     = status.Error(codes.FailedPrecondition, "В группе не может быть больше 10 участников")
	errTooManyTries  = status.Error(codes.ResourceExhausted, "Слишком много неверных кодов. Попробуйте через 5 минут")
	errCodeBusy      = status.Error(codes.Unavailable, "Не удалось создать код. Попробуйте ещё раз")

	uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type server struct {
	db      *pgxpool.Pool
	limiter *attemptLimiter
	proto.UnimplementedGroupsServer
}

func NewServer(pool *pgxpool.Pool) proto.GroupsServer {
	return &server{db: pool, limiter: newAttemptLimiter(maxFailedCodes, failedCodesWin)}
}

func currentUser(ctx context.Context) (string, error) {
	id, ok := interceptor.UserIDFromContext(ctx)
	if !ok {
		return "", errUnauthorized
	}
	return id, nil
}

// roleOf возвращает роль пользователя в группе; для не-участника и несуществующей группы — NotFound.
func roleOf(ctx context.Context, q queryer, groupID, userID string) (string, error) {
	if !uuidRegex.MatchString(groupID) {
		return "", errGroupNotFound
	}
	var role string
	err := q.QueryRow(ctx,
		`SELECT role FROM group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errGroupNotFound
	}
	if err != nil {
		return "", errInternal
	}
	return role, nil
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *server) CreateGroup(ctx context.Context, req *proto.CreateGroupRequest) (*proto.Group, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	name := req.Name
	if name == "" {
		var x int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM group_members WHERE user_id = $1`, userID).Scan(&x); err != nil {
			return nil, errInternal
		}
		name = fmt.Sprintf("Группа %d", x+1)
	}

	var groupID string
	if err := tx.QueryRow(ctx, `INSERT INTO groups (title) VALUES ($1) RETURNING group_id`, name).Scan(&groupID); err != nil {
		return nil, errInternal
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, $3)`, groupID, userID, roleOwner); err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}
	return &proto.Group{GroupId: groupID, Name: name, CreatorId: userID}, nil
}

func (s *server) ListMyGroups(ctx context.Context, _ *proto.ListMyGroupsRequest) (*proto.ListMyGroupsResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx,
		`SELECT g.group_id, g.title, o.user_id
		 FROM group_members m
		 JOIN groups g ON g.group_id = m.group_id
		 JOIN group_members o ON o.group_id = g.group_id AND o.role = 'owner'
		 WHERE m.user_id = $1
		 ORDER BY m.joined_at, g.group_id`, userID)
	if err != nil {
		return nil, errInternal
	}
	defer rows.Close()

	out := &proto.ListMyGroupsResponse{Groups: []*proto.Group{}}
	for rows.Next() {
		var g proto.Group
		if err := rows.Scan(&g.GroupId, &g.Name, &g.CreatorId); err != nil {
			return nil, errInternal
		}
		out.Groups = append(out.Groups, &g)
	}
	if err := rows.Err(); err != nil {
		return nil, errInternal
	}
	return out, nil
}

func (s *server) GetGroup(ctx context.Context, req *proto.GroupIdRequest) (*proto.Group, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uuidRegex.MatchString(req.GroupId) {
		return nil, errGroupNotFound
	}
	var g proto.Group
	err = s.db.QueryRow(ctx,
		`SELECT g.group_id, g.title, o.user_id
		 FROM group_members m
		 JOIN groups g ON g.group_id = m.group_id
		 JOIN group_members o ON o.group_id = g.group_id AND o.role = 'owner'
		 WHERE m.group_id = $1 AND m.user_id = $2`, req.GroupId, userID).Scan(&g.GroupId, &g.Name, &g.CreatorId)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errGroupNotFound
	}
	if err != nil {
		return nil, errInternal
	}
	return &g, nil
}

func (s *server) RenameGroup(ctx context.Context, req *proto.RenameGroupRequest) (*proto.RenameGroupResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	role, err := roleOf(ctx, s.db, req.GroupId, userID)
	if err != nil {
		return nil, err
	}
	if role != roleOwner {
		return nil, errForbidden
	}
	var name string
	err = s.db.QueryRow(ctx,
		`UPDATE groups SET title = $2 WHERE group_id = $1 RETURNING title`, req.GroupId, req.Name).Scan(&name)
	if err != nil {
		return nil, errInternal
	}
	return &proto.RenameGroupResponse{Name: name}, nil
}

// ListMembers: сначала текущий пользователь, затем создатель, затем остальные по дате вступления.
func (s *server) ListMembers(ctx context.Context, req *proto.GroupIdRequest) (*proto.ListMembersResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := roleOf(ctx, s.db, req.GroupId, userID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx,
		`SELECT u.user_id, u.first_name, u.last_name, u.middle_name, m.joined_at, m.income_cents, m.role
		 FROM group_members m
		 JOIN users u ON u.user_id = m.user_id
		 WHERE m.group_id = $1
		 ORDER BY (m.user_id = $2) DESC, (m.role = 'owner') DESC, m.joined_at, u.user_id`,
		req.GroupId, userID)
	if err != nil {
		return nil, errInternal
	}
	defer rows.Close()

	out := &proto.ListMembersResponse{Members: []*proto.Member{}}
	for rows.Next() {
		var (
			m                    proto.Member
			lastName, middleName *string
			joined               time.Time
		)
		if err := rows.Scan(&m.UserId, &m.FirstName, &lastName, &middleName, &joined, &m.Income, &m.Role); err != nil {
			return nil, errInternal
		}
		if lastName != nil {
			m.LastName = *lastName
		}
		if middleName != nil {
			m.MiddleName = *middleName
		}
		m.JoinedAt = fmtTime(joined)
		out.Members = append(out.Members, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, errInternal
	}
	return out, nil
}

func (s *server) GetMyIncome(ctx context.Context, req *proto.GroupIdRequest) (*proto.IncomeResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uuidRegex.MatchString(req.GroupId) {
		return nil, errGroupNotFound
	}
	var income int64
	err = s.db.QueryRow(ctx,
		`SELECT income_cents FROM group_members WHERE group_id = $1 AND user_id = $2`,
		req.GroupId, userID).Scan(&income)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errGroupNotFound
	}
	if err != nil {
		return nil, errInternal
	}
	return &proto.IncomeResponse{Income: income}, nil
}

// SetMyIncome меняет только собственный доход: user_id берётся из токена.
func (s *server) SetMyIncome(ctx context.Context, req *proto.SetMyIncomeRequest) (*proto.IncomeResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if !uuidRegex.MatchString(req.GroupId) {
		return nil, errGroupNotFound
	}
	var income int64
	err = s.db.QueryRow(ctx,
		`UPDATE group_members SET income_cents = $3
		 WHERE group_id = $1 AND user_id = $2 RETURNING income_cents`,
		req.GroupId, userID, req.Income).Scan(&income)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errGroupNotFound
	}
	if err != nil {
		return nil, errInternal
	}
	return &proto.IncomeResponse{Income: income}, nil
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", n.Int64()), nil
}

// CreateInvite: только создатель. Пока есть живой код, возвращается он же; иначе выдаётся новый.
// Использованный код удалён (см. AcceptInvite), значит создатель может сразу получить новый.
func (s *server) CreateInvite(ctx context.Context, req *proto.GroupIdRequest) (*proto.InviteResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	role, err := roleOf(ctx, tx, req.GroupId, userID)
	if err != nil {
		return nil, err
	}
	if role != roleOwner {
		return nil, errForbidden
	}
	// порядок блокировок везде одинаковый: сначала группа, потом строка кода
	if _, err := tx.Exec(ctx, `SELECT 1 FROM groups WHERE group_id = $1 FOR UPDATE`, req.GroupId); err != nil {
		return nil, errInternal
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM group_invites WHERE group_id = $1 AND expires_at <= now()`, req.GroupId); err != nil {
		return nil, errInternal
	}

	var code string
	var expires time.Time
	err = tx.QueryRow(ctx,
		`SELECT code, expires_at FROM group_invites WHERE group_id = $1`, req.GroupId).Scan(&code, &expires)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, errInternal
		}
		return &proto.InviteResponse{Code: code, ExpiresAt: fmtTime(expires)}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, errInternal
	}

	for i := 0; i < inviteCodeTries; i++ {
		code, err = randomCode()
		if err != nil {
			return nil, errInternal
		}
		// код глобально уникален; чужой просроченный код перезаписывается, живой — нет
		err = tx.QueryRow(ctx,
			`INSERT INTO group_invites (group_id, code, expires_at)
			 VALUES ($1, $2, now() + make_interval(secs => $3))
			 ON CONFLICT (code) DO UPDATE
			   SET group_id = EXCLUDED.group_id, expires_at = EXCLUDED.expires_at
			   WHERE group_invites.expires_at <= now()
			 RETURNING expires_at`,
			req.GroupId, code, inviteTTL.Seconds()).Scan(&expires)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // код занят живым приглашением другой группы
		}
		if err != nil {
			return nil, errInternal
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, errInternal
		}
		return &proto.InviteResponse{Code: code, ExpiresAt: fmtTime(expires)}, nil
	}
	return nil, errCodeBusy
}

// AcceptInvite: код одноразовый. Всё в одной транзакции: при ошибке (уже участник, группа полна)
// код не сгорает. Неверные коды считаются в лимитере (5 за 5 минут на пользователя).
func (s *server) AcceptInvite(ctx context.Context, req *proto.AcceptInviteRequest) (*proto.AcceptInviteResponse, error) {
	userID, err := currentUser(ctx)
	if err != nil {
		return nil, err
	}
	if s.limiter.blocked(userID) {
		return nil, errTooManyTries
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var groupID string
	err = tx.QueryRow(ctx,
		`SELECT group_id FROM group_invites WHERE code = $1 AND expires_at > now()`, req.Code).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		s.limiter.fail(userID)
		return nil, errCodeNotFound
	}
	if err != nil {
		return nil, errInternal
	}

	// блокируем группу: сериализует одновременные вступления (лимит участников) и совпадает по порядку с CreateInvite
	if _, err := tx.Exec(ctx, `SELECT 1 FROM groups WHERE group_id = $1 FOR UPDATE`, groupID); err != nil {
		return nil, errInternal
	}
	// одноразовость: удаляем код атомарно; если параллельный запрос успел раньше, строки уже нет
	var used string
	err = tx.QueryRow(ctx,
		`DELETE FROM group_invites WHERE code = $1 AND group_id = $2 AND expires_at > now() RETURNING group_id`,
		req.Code, groupID).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		s.limiter.fail(userID)
		return nil, errCodeNotFound
	}
	if err != nil {
		return nil, errInternal
	}

	var already bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2)`,
		groupID, userID).Scan(&already); err != nil {
		return nil, errInternal
	}
	if already {
		return nil, errAlreadyMember
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM group_members WHERE group_id = $1`, groupID).Scan(&count); err != nil {
		return nil, errInternal
	}
	if count >= maxMembers {
		return nil, errGroupFull
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, $3)`, groupID, userID, roleMember); err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}
	return &proto.AcceptInviteResponse{GroupId: groupID}, nil
}

var _ proto.GroupsServer = (*server)(nil)
