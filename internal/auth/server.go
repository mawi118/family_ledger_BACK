package auth

import (
	"context"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/internal/token"
	"github.com/mawi118/family_ledger_BACK/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type server struct {
	db        *pgxpool.Pool
	jwtSecret []byte
	jwtTTL    time.Duration
	proto.UnimplementedAuthServer
}

func NewServer(pool *pgxpool.Pool, jwtSecret []byte, jwtTTL time.Duration) proto.AuthServer {
	return &server{db: pool}
}

func (s *server) Register(ctx context.Context, req *proto.RegisterRequest) (*proto.RegisterResponse, error) {
	hash, err := argon2id.CreateHash(req.Password, argon2id.DefaultParams)
	if err != nil {
		return nil, err
	}
	var userID string
	err = s.db.QueryRow(ctx,
		"	INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING user_id",
		req.Email, hash).Scan(&userID)

	if err != nil {
		return nil, err
	}

	return &proto.RegisterResponse{UserId: userID}, nil
}
func (s *server) EmailExists(ctx context.Context, req *proto.EmailExistsRequest) (*proto.EmailExistsResponse, error) {
	var exists bool
	err := s.db.QueryRow(ctx, "	SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)", req.Email).Scan(&exists)

	if err != nil {
		return nil, err
	}
	return &proto.EmailExistsResponse{Exists: exists}, nil
}
func (s *server) Login(ctx context.Context, req *proto.LoginRequest) (*proto.LoginResponse, error) {
	var userID, hash string
	err := s.db.QueryRow(ctx,
		"SELECT user_id, password_hash FROM users WHERE email = $1",
		req.Email,
	).Scan(&userID, &hash)
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

	tok, err := token.Generate(userID, s.jwtSecret, s.jwtTTL)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &proto.LoginResponse{Token: tok}, nil
}

var _ proto.AuthServer = (*server)(nil)
