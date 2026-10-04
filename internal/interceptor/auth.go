package interceptor

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawi118/family_ledger_BACK/internal/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type ctxKey struct{}

// Методы без авторизации. Всё остальное требует Bearer access-токен (запрет по умолчанию).
// Auth.Me принимает токен в теле запроса и проверяет его сам.
var publicMethods = map[string]bool{
	"/FL.v1.Auth/Register":      true,
	"/FL.v1.Auth/Login":         true,
	"/FL.v1.Auth/EmailExists":   true,
	"/FL.v1.Auth/Refresh":       true,
	"/FL.v1.Auth/Logout":        true,
	"/FL.v1.Auth/Me":            true,
	"/FL.v1.Health/HealthCheck": true,
}

var errUnauthorized = status.Error(codes.Unauthenticated, "Не авторизован")

// UserIDFromContext возвращает id пользователя, подтверждённый AuthInterceptor.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok && id != ""
}

// WithUserID нужен тестам и внутреннему коду, которым надо подставить пользователя вручную.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, userID)
}

// NewAuthInterceptor проверяет access-токен: подпись, срок и то, что его refresh-строка (jti) жива.
// Тот же критерий, что и в Auth.Me: logout/login/refresh мгновенно обрывают старый access.
// user_id в handler приходит только из проверенного токена, никогда из тела запроса.
func NewAuthInterceptor(pool *pgxpool.Pool, jwtSecret []byte) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if publicMethods[info.FullMethod] || strings.HasPrefix(info.FullMethod, "/grpc.reflection.") {
			return handler(ctx, req)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, errUnauthorized
		}
		vals := md.Get("authorization")
		if len(vals) == 0 {
			return nil, errUnauthorized
		}
		const prefix = "Bearer "
		if len(vals[0]) <= len(prefix) || !strings.EqualFold(vals[0][:len(prefix)], prefix) {
			return nil, errUnauthorized
		}

		claims, err := token.ParseAccess(strings.TrimSpace(vals[0][len(prefix):]), jwtSecret)
		if err != nil {
			return nil, errUnauthorized
		}

		var one int
		err = pool.QueryRow(ctx,
			`SELECT 1 FROM refresh_tokens
			 WHERE token_id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > now()`,
			claims.ID, claims.Subject).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errUnauthorized
		}
		if err != nil {
			return nil, status.Error(codes.Internal, "internal error")
		}

		return handler(WithUserID(ctx, claims.Subject), req)
	}
}
