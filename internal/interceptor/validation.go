package interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Validator interface {
	Validate() error
}

// Normalizer приводит запрос к каноническому виду (trim, регистр и т.п.) до валидации.
type Normalizer interface {
	Normalize()
}

func ValidationInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if n, ok := req.(Normalizer); ok {
		n.Normalize()
	}
	if v, ok := req.(Validator); ok {
		if err := v.Validate(); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	return handler(ctx, req)
}
