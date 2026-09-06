// реализация биз.логики gRPC метода
package health

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/proto"
)

type server struct {
	db *pgxpool.Pool
	proto.UnimplementedHealthServer
}

func (s *server) HealthCheck(ctx context.Context, req *proto.HealthRequest) (*proto.HealthResponse, error) {
	if err := s.db.Ping(ctx); err != nil {
		return nil, err
	}
	return &proto.HealthResponse{Code: 200}, nil
}

var _ proto.HealthServer = (*server)(nil)

func NewServer(pool *pgxpool.Pool) proto.HealthServer {
	return &server{db: pool}
}
