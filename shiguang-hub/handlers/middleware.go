package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// RequireTenant is a Fiber middleware that extracts and validates the operator
// JWT. On success, sets c.Locals("tenant_id") for downstream handlers.
func RequireTenant(jwtSecret, jwtIssuer string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")

		claims := jwt.MapClaims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(jwtSecret), nil
		}, jwt.WithIssuer(jwtIssuer))

		if err != nil || !token.Valid {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}

		tenantID, ok := claims["tenant_id"].(string)
		if !ok || tenantID == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "missing tenant_id in token")
		}

		c.Locals("tenant_id", tenantID)
		return c.Next()
	}
}
