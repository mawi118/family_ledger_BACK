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
	var code int32
	err := s.db.QueryRow(ctx, "SELECT code FROM grpc_test_table LIMIT 1").Scan(&code)
	if err != nil {
		return nil, err
	}
	return &proto.HealthResponse{Code: code}, nil
}

var _ proto.HealthServer = (*server)(nil)

func NewServer(pool *pgxpool.Pool) proto.HealthServer {
	return &server{db: pool}
}
