package auth

import (
	"context"

	"github.com/alexedwards/argon2id"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/proto"
)

// argon2id.CreateHash(password, params) -
// возвращает уже готовую строку для хранения в БД (соль и параметры зашиты внутрь неё же)
// argon2id.ComparePasswordAndHash(password, hash)
// для проверки при логине (понадобится позже, не сейчас).
type server struct {
	db *pgxpool.Pool
	proto.UnimplementedAuthServer
}

func NewServer(pool *pgxpool.Pool) proto.AuthServer {
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

var _ proto.AuthServer = (*server)(nil)
