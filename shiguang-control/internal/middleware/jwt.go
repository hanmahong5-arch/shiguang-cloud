// Package middleware contains Fiber middleware used by the handlers.
package middleware

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// JWTConfig bundles the values needed for signing + verifying admin tokens.
type JWTConfig struct {
	Secret  string        // HS256 signing key
	Issuer  string        // expected iss claim
	TTLDays int           // token lifetime (used by Issue)
	Leeway  time.Duration // clock skew tolerance
}

// Claims is the minimal admin token payload.
type Claims struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	jwt.RegisteredClaims
}

// Issue returns a signed JWT string for the given admin username.
func Issue(cfg JWTConfig, adminName string) (string, error) {
	now := time.Now()
	claims := Claims{
		Subject: adminName,
		Role:    "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    cfg.Issuer,
			Subject:   adminName,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(cfg.TTLDays) * 24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Secret))
}

// RequireAdmin returns a Fiber middleware that enforces a valid admin JWT.
// The token is read from the Authorization: Bearer <token> header.
func RequireAdmin(cfg JWTConfig) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(cfg.Secret), nil
		}, jwt.WithIssuer(cfg.Issuer), jwt.WithLeeway(cfg.Leeway))

		if err != nil || !token.Valid {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}
		if claims.Role != "admin" {
			return fiber.NewError(fiber.StatusForbidden, "admin role required")
		}
		c.Locals("admin", claims.Subject)
		return c.Next()
	}
}
