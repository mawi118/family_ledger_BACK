package proto

import "context"

type server struct {
	UnimplementedHealthServer
}

func (s *server) HealthCheck(ctx context.Context, req *HealthRequest) (*HealthResponse, error) {
	return &HealthResponse{Code: 0}, nil
}

var _ HealthServer = (*server)(nil)
