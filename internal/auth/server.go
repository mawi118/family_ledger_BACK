package auth

import (
	"context"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/internal/token"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const pgUniqueViolation = "23505"

var (
	// Единый ответ на любую неуспешную авторизацию (док 3): не раскрываем, существует ли аккаунт.
	errInvalidCredentials = status.Error(codes.Unauthenticated, "Неверное имя пользователя или пароль")
	errUnauthorized       = status.Error(codes.Unauthenticated, "Не авторизован")
	errInternal           = status.Error(codes.Internal, "internal error")
)

type server struct {
	db         *pgxpool.Pool
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	// dummyHash нужен, чтобы Login для несуществующей почты тратил столько же времени,
	// сколько для существующей (иначе по времени ответа видно, есть ли аккаунт).
	dummyHash string
	proto.UnimplementedAuthServer
}

func NewServer(pool *pgxpool.Pool, jwtSecret []byte, accessTTL, refreshTTL time.Duration) proto.AuthServer {
	dummy, err := argon2id.CreateHash("dummy-password-for-timing-only", argon2id.DefaultParams)
	if err != nil {
		panic("auth: cannot create dummy hash: " + err.Error())
	}
	return &server{db: pool, jwtSecret: jwtSecret, accessTTL: accessTTL, refreshTTL: refreshTTL, dummyHash: dummy}
}

// lockUser сериализует операции с сессиями одного пользователя (Login/Refresh/Logout),
// чтобы параллельные запросы не оставили у него больше одной живой сессии.
// Блокировка снимается автоматически в конце транзакции.
func lockUser(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID)
	return err
}

// issueTokens создаёт новую refresh-строку и access-токен, привязанный к ней через jti.
// Срок жизни refresh считается на стороне БД (now()), а не по часам Go.
func (s *server) issueTokens(ctx context.Context, tx pgx.Tx, userID string) (access, refresh string, err error) {
	raw, hash, err := token.GenerateRefresh()
	if err != nil {
		return "", "", err
	}
	var sessionID string
	err = tx.QueryRow(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, now() + make_interval(secs => $3))
		 RETURNING token_id`,
		userID, hash, s.refreshTTL.Seconds()).Scan(&sessionID)
	if err != nil {
		return "", "", err
	}
	access, err = token.GenerateAccess(userID, sessionID, s.jwtSecret, s.accessTTL)
	if err != nil {
		return "", "", err
	}
	return access, raw, nil
}

// revokeAll отзывает все живые refresh-токены пользователя (а вместе с ними и привязанные access).
func revokeAll(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

func (s *server) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	hash, err := argon2id.CreateHash(req.Password, argon2id.DefaultParams)
	if err != nil {
		return nil, errInternal
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	err = tx.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, first_name) VALUES ($1, $2, $3) RETURNING user_id",
		req.Email, hash, req.FirstName).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return nil, status.Error(codes.AlreadyExists, "Почта уже занята")
		}
		return nil, errInternal
	}

	accessTok, refreshTok, err := s.issueTokens(ctx, tx, userID)
	if err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}

	return &proto.RegisterResponse{
		AccessToken:  accessTok,
		RefreshToken: refreshTok,
		User: &proto.User{
			UserId:    userID,
			Email:     req.Email,
			FirstName: req.FirstName,
		},
	}, nil
}

func (s *server) EmailExists(ctx context.Context, req *proto.EmailExistsRequest) (*proto.EmailExistsResponse, error) {
	var exists bool
	err := s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)
	if err != nil {
		return nil, errInternal
	}
	return &proto.EmailExistsResponse{Exists: exists}, nil
}

func (s *server) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	var userID, hash, firstName string
	err := s.db.QueryRow(ctx,
		"SELECT user_id, password_hash, first_name FROM users WHERE email = $1",
		req.Email,
	).Scan(&userID, &hash, &firstName)
	if errors.Is(err, pgx.ErrNoRows) {
		// тратим столько же времени, сколько на реального пользователя
		_, _ = argon2id.ComparePasswordAndHash(req.Password, s.dummyHash)
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, errInternal
	}

	match, err := argon2id.ComparePasswordAndHash(req.Password, hash)
	if err != nil {
		return nil, errInternal
	}
	if !match {
		return nil, errInvalidCredentials
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockUser(ctx, tx, userID); err != nil {
		return nil, errInternal
	}
	// одна сессия на пользователя: прошлые refresh (и привязанные к ним access) перестают действовать
	if err := revokeAll(ctx, tx, userID); err != nil {
		return nil, errInternal
	}
	accessTok, refreshTok, err := s.issueTokens(ctx, tx, userID)
	if err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}

	return &proto.LoginResponse{
		AccessToken:  accessTok,
		RefreshToken: refreshTok,
		User: &proto.User{
			UserId:    userID,
			Email:     req.Email,
			FirstName: firstName,
		},
	}, nil
}

func (s *server) Refresh(ctx context.Context, req *proto.RefreshRequest) (*proto.RefreshResponse, error) {
	hash := token.HashRefresh(req.RefreshToken)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// сначала узнаём владельца токена, чтобы взять блокировку на пользователя
	var userID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM refresh_tokens WHERE token_hash = $1`, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, errInternal
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return nil, errInternal
	}

	// атомарная проверка и ревокация: из двух параллельных запросов с одним токеном пройдёт ровно один
	err = tx.QueryRow(ctx,
		`UPDATE refresh_tokens SET revoked_at = now()
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		 RETURNING user_id`, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, errInternal
	}

	// ротация: новая пара; старый access перестаёт действовать вместе со старой refresh-строкой
	accessTok, refreshTok, err := s.issueTokens(ctx, tx, userID)
	if err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}

	return &proto.RefreshResponse{AccessToken: accessTok, RefreshToken: refreshTok}, nil
}

// Logout отзывает все токены пользователя. Неизвестный токен — не ошибка (идемпотентность).
func (s *server) Logout(ctx context.Context, req *proto.LogoutRequest) (*proto.LogoutResponse, error) {
	hash := token.HashRefresh(req.RefreshToken)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, errInternal
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM refresh_tokens WHERE token_hash = $1`, hash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &proto.LogoutResponse{Success: true}, nil
	}
	if err != nil {
		return nil, errInternal
	}
	if err := lockUser(ctx, tx, userID); err != nil {
		return nil, errInternal
	}
	if err := revokeAll(ctx, tx, userID); err != nil {
		return nil, errInternal
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errInternal
	}
	return &proto.LogoutResponse{Success: true}, nil
}

// Me принимает access, только если подпись и срок в порядке И его refresh-строка (jti) жива.
// Данные пользователя читаются из БД, а не из claims.
func (s *server) Me(ctx context.Context, req *proto.MeRequest) (*proto.MeResponse, error) {
	claims, err := token.ParseAccess(req.AccessToken, s.jwtSecret)
	if err != nil {
		return nil, errUnauthorized
	}

	var email, firstName string
	err = s.db.QueryRow(ctx,
		`SELECT u.email, u.first_name
		 FROM refresh_tokens t
		 JOIN users u ON u.user_id = t.user_id
		 WHERE t.token_id = $1 AND t.user_id = $2
		   AND t.revoked_at IS NULL AND t.expires_at > now()`,
		claims.ID, claims.Subject,
	).Scan(&email, &firstName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errUnauthorized
	}
	if err != nil {
		return nil, errInternal
	}

	return &proto.MeResponse{
		User: &proto.User{
			UserId:    claims.Subject,
			Email:     email,
			FirstName: firstName,
		},
	}, nil
}

var _ proto.AuthServer = (*server)(nil)
