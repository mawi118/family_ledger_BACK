package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GenerateAccess — короткоживущий JWT, как и раньше. Проверяется без похода в БД.
func GenerateAccess(userID string, secret []byte, ttl time.Duration) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tok.SignedString(secret)
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
