package auth

import (
	"context"
	"errors"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/internal/token"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const pgUniqueViolation = "23505"

type server struct {
	db         *pgxpool.Pool
	jwtSecret  []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	proto.UnimplementedAuthServer
}

func NewServer(pool *pgxpool.Pool, jwtSecret []byte, accessTTL, refreshTTL time.Duration) proto.AuthServer {
	return &server{db: pool, jwtSecret: jwtSecret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (s *server) issueTokens(ctx context.Context, userID string) (access, refresh string, err error) {
	access, err = token.GenerateAccess(userID, s.jwtSecret, s.accessTTL)
	if err != nil {
		return "", "", err
	}
	raw, hash, err := token.GenerateRefresh()
	if err != nil {
		return "", "", err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hash, time.Now().Add(s.refreshTTL))
	if err != nil {
		return "", "", err
	}
	return access, raw, nil
}

func (s *server) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	hash, err := argon2id.CreateHash(req.Password, argon2id.DefaultParams)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	var userID string
	err = s.db.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, first_name) VALUES ($1, $2, $3) RETURNING user_id",
		req.Email, hash, req.FirstName).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	accessTok, refreshTok, err := s.issueTokens(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
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
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &proto.EmailExistsResponse{Exists: exists}, nil
}

func (s *server) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	var userID, hash, firstName string
	err := s.db.QueryRow(ctx,
		"SELECT user_id, password_hash, COALESCE(first_name, '') FROM users WHERE email = $1",
		req.Email,
	).Scan(&userID, &hash, &firstName)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid email or password")
	}

	match, err := argon2id.ComparePasswordAndHash(req.Password, hash)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	if !match {
		return nil, status.Error(codes.Unauthenticated, "invalid email or password")
	}

	accessTok, refreshTok, err := s.issueTokens(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
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

	var userID string
	var expiresAt time.Time
	var revokedAt *time.Time
	err := s.db.QueryRow(ctx,
		`SELECT user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = $1`,
		hash,
	).Scan(&userID, &expiresAt, &revokedAt)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if revokedAt != nil || time.Now().After(expiresAt) {
		return nil, status.Error(codes.Unauthenticated, "refresh token expired or revoked")
	}

	// ротация: старый refresh гасим, выдаём новую пару
	if _, err := s.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`, hash); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	accessTok, refreshTok, err := s.issueTokens(ctx, userID)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &proto.RefreshResponse{AccessToken: accessTok, RefreshToken: refreshTok}, nil
}

func (s *server) Logout(ctx context.Context, req *proto.LogoutRequest) (*proto.LogoutResponse, error) {
	hash := token.HashRefresh(req.RefreshToken)
	_, err := s.db.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`, hash)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	return &proto.LogoutResponse{Success: true}, nil
}

func (s *server) Me(ctx context.Context, req *proto.MeRequest) (*proto.MeResponse, error) {
	claims := &jwt.RegisteredClaims{}
	tok, err := jwt.ParseWithClaims(req.AccessToken, claims, func(t *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil || !tok.Valid {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
	}

	var email, firstName string
	err = s.db.QueryRow(ctx,
		"SELECT email, COALESCE(first_name, '') FROM users WHERE user_id = $1",
		claims.Subject,
	).Scan(&email, &firstName)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "user not found")
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
