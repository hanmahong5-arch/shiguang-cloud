package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiguang/gate/internal/defense"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	bl, err := defense.NewBanList(filepath.Join(t.TempDir(), "bans.json"))
	if err != nil {
		t.Fatal(err)
	}
	rl := defense.NewRateLimiter(1, 1, 0)
	return NewServer("test-gate", "127.0.0.1:0", bl, rl, nil)
}

func TestAdmin_Healthz(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.handleHealthz(w, req)
	if w.Code != 200 {
		t.Errorf("code=%d", w.Code)
	}
	// Response is now JSON with status/routes/active_conn/ban_count
	body := w.Body.String()
	if !strings.Contains(body, `"status":"healthy"`) {
		t.Errorf("expected healthy status in body=%q", body)
	}
}

func TestAdmin_BanAndUnban(t *testing.T) {
	s := newTestServer(t)

	// Ban
	body, _ := json.Marshal(banRequest{IP: "1.2.3.4", Reason: "test"})
	req := httptest.NewRequest(http.MethodPost, "/ban", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleBan(w, req)
	if w.Code != 200 {
		t.Errorf("ban code=%d body=%s", w.Code, w.Body.String())
	}
	if !s.banlist.IsBanned("1.2.3.4") {
		t.Error("IP not banned after POST /ban")
	}

	// Unban
	body, _ = json.Marshal(unbanRequest{IP: "1.2.3.4"})
	req = httptest.NewRequest(http.MethodPost, "/unban", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleUnban(w, req)
	if w.Code != 200 {
		t.Errorf("unban code=%d", w.Code)
	}
	if s.banlist.IsBanned("1.2.3.4") {
		t.Error("IP still banned after POST /unban")
	}
}

func TestAdmin_BanRejectsNonPost(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/ban", nil)
	w := httptest.NewRecorder()
	s.handleBan(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code=%d want 405", w.Code)
	}
}

func TestAdmin_BanRequiresIP(t *testing.T) {
	s := newTestServer(t)
	body := strings.NewReader(`{"reason":"no ip"}`)
	req := httptest.NewRequest(http.MethodPost, "/ban", body)
	w := httptest.NewRecorder()
	s.handleBan(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code=%d want 400", w.Code)
	}
}

func TestAdmin_Status(t *testing.T) {
	s := newTestServer(t)
	s.banlist.Ban("1.1.1.1", "test", 0)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)
	if w.Code != 200 {
		t.Errorf("code=%d", w.Code)
	}
	var resp statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Instance != "test-gate" {
		t.Errorf("instance=%q", resp.Instance)
	}
	if resp.BanCount != 1 {
		t.Errorf("ban_count=%d want 1", resp.BanCount)
	}
}
