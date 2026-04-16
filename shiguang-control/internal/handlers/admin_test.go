package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/shiguang/control/internal/config"
	"github.com/shiguang/control/internal/hub"
	"github.com/shiguang/control/internal/middleware"
)

func setupAdmin(t *testing.T, gates []config.GateEndpoint) (*fiber.App, string) {
	t.Helper()
	jwtCfg := middleware.JWTConfig{
		Secret:  "test_secret_at_least_16_chars",
		Issuer:  "test",
		TTLDays: 1,
	}
	h := hub.NewHub()
	go h.Run()

	admin := NewAdminHandler(jwtCfg, gates, h, "admin", "pw")
	app := fiber.New()
	admin.Register(app.Group("/api"))

	// Log in to obtain a token
	body, _ := json.Marshal(loginReq{Username: "admin", Password: "pw"})
	req := httptest.NewRequest("POST", "/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
	var out map[string]string
	raw, _ := io.ReadAll(resp.Body)
	json.Unmarshal(raw, &out)
	return app, out["token"]
}

func TestAdmin_LoginSuccessAndBadPassword(t *testing.T) {
	app, token := setupAdmin(t, nil)
	if token == "" {
		t.Fatal("no token returned")
	}

	// Bad password
	body, _ := json.Marshal(loginReq{Username: "admin", Password: "wrong"})
	req := httptest.NewRequest("POST", "/api/admin/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("bad password code=%d want 401", resp.StatusCode)
	}
}

func TestAdmin_RequiresJWT(t *testing.T) {
	app, _ := setupAdmin(t, nil)
	req := httptest.NewRequest("GET", "/api/admin/online", nil)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 401 {
		t.Errorf("unauth code=%d", resp.StatusCode)
	}
}

func TestAdmin_Online(t *testing.T) {
	app, token := setupAdmin(t, nil)
	req := httptest.NewRequest("GET", "/api/admin/online", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 200 {
		t.Errorf("code=%d", resp.StatusCode)
	}
}

func TestAdmin_BanFansOutToGates(t *testing.T) {
	// Stand up a fake gate HTTP server
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ban") && r.Method == http.MethodPost {
			calls++
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "1.2.3.4") {
				t.Errorf("body missing ip: %s", body)
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer fake.Close()

	app, token := setupAdmin(t, []config.GateEndpoint{{Name: "fake", URL: fake.URL}})

	banBody, _ := json.Marshal(banReq{IP: "1.2.3.4", Reason: "test"})
	req := httptest.NewRequest("POST", "/api/admin/ban", bytes.NewReader(banBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("ban code=%d body=%s", resp.StatusCode, body)
	}
	if calls != 1 {
		t.Errorf("expected 1 gate call, got %d", calls)
	}
}
