package middleware

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

func newCfg() JWTConfig {
	return JWTConfig{
		Secret:  "test_secret_at_least_16_chars",
		Issuer:  "test",
		TTLDays: 1,
	}
}

func TestJWT_IssueAndVerify(t *testing.T) {
	cfg := newCfg()
	token, err := Issue(cfg, "admin1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(token) < 50 {
		t.Errorf("token too short: %d", len(token))
	}
}

func TestJWT_MiddlewareAccepts(t *testing.T) {
	cfg := newCfg()
	token, _ := Issue(cfg, "admin1")

	app := fiber.New()
	app.Get("/p", RequireAdmin(cfg), func(c *fiber.Ctx) error {
		return c.SendString(c.Locals("admin").(string))
	})

	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 200 {
		t.Errorf("code=%d", resp.StatusCode)
	}
}

func TestJWT_MiddlewareRejectsMissing(t *testing.T) {
	cfg := newCfg()
	app := fiber.New()
	app.Get("/p", RequireAdmin(cfg), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	req := httptest.NewRequest("GET", "/p", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("code=%d want 401", resp.StatusCode)
	}
}

func TestJWT_MiddlewareRejectsBadToken(t *testing.T) {
	cfg := newCfg()
	app := fiber.New()
	app.Get("/p", RequireAdmin(cfg), func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer complete.garbage.token")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("code=%d want 401", resp.StatusCode)
	}
}

func TestJWT_TTLRespected(t *testing.T) {
	cfg := newCfg()
	cfg.TTLDays = 0 // will produce an already-expired token
	token, _ := Issue(cfg, "a")

	app := fiber.New()
	app.Get("/p", RequireAdmin(cfg), func(c *fiber.Ctx) error { return c.SendString("ok") })
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	// Small delay to ensure exp < now
	time.Sleep(20 * time.Millisecond)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("expected 401 on expired token, got %d", resp.StatusCode)
	}
}
