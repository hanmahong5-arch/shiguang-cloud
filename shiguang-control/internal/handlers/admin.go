package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/shiguang/control/internal/config"
	"github.com/shiguang/control/internal/hub"
	"github.com/shiguang/control/internal/middleware"
)

// AdminHandler exposes JWT-protected admin APIs for managing bans,
// running status queries, and kicking live launcher sessions.
type AdminHandler struct {
	jwtCfg middleware.JWTConfig
	gates  []config.GateEndpoint
	hub    *hub.Hub
	client *http.Client

	// Stored admin credentials (simple single-operator model for now).
	// A full version would use a dedicated admin DB. For private server
	// operation, one operator account backed by config is enough.
	adminUser string
	adminPass string

	loginRL *ipRateLimiter // 10 admin login attempts per IP per 5 minutes
}

// NewAdminHandler wires the handler.
func NewAdminHandler(jwtCfg middleware.JWTConfig, gates []config.GateEndpoint, h *hub.Hub, adminUser, adminPass string) *AdminHandler {
	return &AdminHandler{
		jwtCfg:    jwtCfg,
		gates:     gates,
		hub:       h,
		client:    &http.Client{Timeout: 5 * time.Second},
		adminUser: adminUser,
		adminPass: adminPass,
		loginRL:   newIPRateLimiter(5*time.Minute, 10),
	}
}

// Register attaches /api/admin/* routes.
func (h *AdminHandler) Register(r fiber.Router) {
	g := r.Group("/admin")
	g.Post("/login", h.login) // unauthenticated
	// authenticated subgroup
	auth := g.Use(middleware.RequireAdmin(h.jwtCfg))
	auth.Get("/status", h.status)
	auth.Get("/online", h.online)
	auth.Post("/ban", h.ban)
	auth.Post("/unban", h.unban)
	auth.Get("/banlist", h.banlist)
	auth.Post("/kick", h.kick)
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AdminHandler) login(c *fiber.Ctx) error {
	// Rate limit: 10 admin login attempts per IP per 5 minutes
	if !h.loginRL.allow(c.IP()) {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many login attempts, try again later")
	}
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if req.Username != h.adminUser || req.Password != h.adminPass {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	}
	token, err := middleware.Issue(h.jwtCfg, req.Username)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "token issuance failed")
	}
	return c.JSON(fiber.Map{"token": token})
}

// status — aggregates /status from every configured gate + hub counts.
func (h *AdminHandler) status(c *fiber.Ctx) error {
	results := make(map[string]any)
	for _, g := range h.gates {
		data, err := h.gateGet(g.URL + "/status")
		if err != nil {
			results[g.Name] = fiber.Map{"error": err.Error()}
			continue
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			results[g.Name] = string(data)
			continue
		}
		results[g.Name] = parsed
	}
	return c.JSON(fiber.Map{
		"gates":           results,
		"launcher_online": h.hub.ConnectedCount(),
		"launcher_served": h.hub.TotalServed(),
	})
}

func (h *AdminHandler) online(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"count": h.hub.ConnectedCount()})
}

type banReq struct {
	IP         string `json:"ip"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"duration_ms"`
	Gate       string `json:"gate,omitempty"` // optional: target a specific gate by name
}

// ban — propagates to all configured gates (or just one if Gate specified).
func (h *AdminHandler) ban(c *fiber.Ctx) error {
	var req banReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if req.IP == "" {
		return fiber.NewError(fiber.StatusBadRequest, "ip required")
	}
	body, _ := json.Marshal(req)
	return h.fanOut(c, "/ban", body, req.Gate)
}

type unbanReq struct {
	IP   string `json:"ip"`
	Gate string `json:"gate,omitempty"`
}

func (h *AdminHandler) unban(c *fiber.Ctx) error {
	var req unbanReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	body, _ := json.Marshal(req)
	return h.fanOut(c, "/unban", body, req.Gate)
}

func (h *AdminHandler) banlist(c *fiber.Ctx) error {
	all := make(map[string]any)
	for _, g := range h.gates {
		data, err := h.gateGet(g.URL + "/banlist")
		if err != nil {
			all[g.Name] = fiber.Map{"error": err.Error()}
			continue
		}
		var parsed any
		json.Unmarshal(data, &parsed)
		all[g.Name] = parsed
	}
	return c.JSON(all)
}

type kickReq struct {
	ClientID string `json:"client_id"`
	Reason   string `json:"reason"`
}

func (h *AdminHandler) kick(c *fiber.Ctx) error {
	var req kickReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	ok := h.hub.Kick(req.ClientID, req.Reason)
	return c.JSON(fiber.Map{"ok": ok})
}

// gateGet helper — GET from a gate's admin API with short timeout.
func (h *AdminHandler) gateGet(url string) ([]byte, error) {
	resp, err := h.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// gatePost helper — POST JSON to a gate's admin API.
func (h *AdminHandler) gatePost(url string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// fanOut posts the same body to every gate (or just one if gateName).
func (h *AdminHandler) fanOut(c *fiber.Ctx, path string, body []byte, gateName string) error {
	out := make(map[string]any)
	matched := false
	for _, g := range h.gates {
		if gateName != "" && g.Name != gateName {
			continue
		}
		matched = true
		data, err := h.gatePost(g.URL+path, body)
		if err != nil {
			out[g.Name] = fiber.Map{"error": err.Error()}
			continue
		}
		var parsed any
		json.Unmarshal(data, &parsed)
		out[g.Name] = parsed
	}
	if !matched {
		return fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("no matching gate: %q", gateName))
	}
	return c.JSON(out)
}
