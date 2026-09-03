// реализация биз.логики gRPC метода
package proto

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct {
	db *pgxpool.Pool
	UnimplementedHealthServer
}

func (s *server) HealthCheck(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	var code int32
	err := s.db.QueryRow(ctx, "SELECT code FROM grpc_test_table LIMIT 1").Scan(&code)
	if err != nil {
		return nil, err
	}
	return &HealthResponse{Code: code}, nil
}

var _ HealthServer = (*server)(nil)

func NewServer(pool *pgxpool.Pool) HealthServer {
	return &server{db: pool}
}
