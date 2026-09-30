package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidAccess — access-токен не прошёл проверку подписи, алгоритма, срока или неполон.
var ErrInvalidAccess = errors.New("invalid access token")

// GenerateAccess — короткоживущий JWT (HS256). sessionID — это refresh_tokens.token_id
// той refresh-строки, к которой привязан access: пока строка не отозвана и не истекла,
// access принимается (см. auth.Me). Кладётся в стандартный claim jti.
func GenerateAccess(userID, sessionID string, secret []byte, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ID:        sessionID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseAccess проверяет подпись (строго HS256), наличие и срок exp, а также наличие sub и jti.
// Токены без jti (выданные до привязки к сессии) отклоняются.
func ParseAccess(raw string, secret []byte) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (interface{}, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !tok.Valid || claims.Subject == "" || claims.ID == "" {
		return nil, ErrInvalidAccess
	}
	return claims, nil
}

// GenerateRefresh — случайная строка (не JWT). Возвращает сырое значение (отдать клиенту)
// и его хэш (положить в БД). Отзываемость обеспечивается записью в БД, а не самим токеном.
func GenerateRefresh() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashRefresh(raw), nil
}

func HashRefresh(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
