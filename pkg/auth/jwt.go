package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken wraps any failure to parse/validate a session token,
// so API handlers can uniformly respond with 401.
var ErrInvalidToken = errors.New("auth: invalid or expired token")

// Claims is the JWT payload used for browser (HTMX) session cookies.
// API tokens (pkg/storage.APIToken) are a separate, opaque-string
// mechanism and do not use JWTs.
type Claims struct {
	UserID        string `json:"uid"`
	IsGlobalAdmin bool   `json:"adm"`
	jwt.RegisteredClaims
}

// TokenIssuer issues and validates HMAC-signed session JWTs.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenIssuer builds an issuer. secret must be non-empty; ttl controls
// how long an issued session remains valid.
func NewTokenIssuer(secret []byte, ttl time.Duration) (*TokenIssuer, error) {
	if len(secret) == 0 {
		return nil, errors.New("auth: signing secret must not be empty")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &TokenIssuer{secret: secret, ttl: ttl}, nil
}

// Issue creates a signed session token for the given user.
func (i *TokenIssuer) Issue(userID string, isGlobalAdmin bool) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:        userID,
		IsGlobalAdmin: isGlobalAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.ttl)),
			Subject:   userID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Parse validates a signed token and returns its claims.
func (i *TokenIssuer) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
