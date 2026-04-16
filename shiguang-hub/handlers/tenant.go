// Package handlers contains Hub REST API handlers for tenant management,
// public bootstrap endpoints, and operator dashboard APIs.
package handlers

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/shiguang/shared/tenant"
	"golang.org/x/crypto/bcrypt"
)

// ipRateLimiter provides simple per-IP rate limiting for sensitive endpoints.
type ipRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	window   time.Duration
	maxHits  int
}

func newIPRateLimiter(window time.Duration, maxHits int) *ipRateLimiter {
	rl := &ipRateLimiter{
		attempts: make(map[string][]time.Time),
		window:   window,
		maxHits:  maxHits,
	}
	// Background cleanup every 60 seconds
	go func() {
		for range time.Tick(60 * time.Second) {
			rl.mu.Lock()
			now := time.Now()
			for ip, times := range rl.attempts {
				var fresh []time.Time
				for _, t := range times {
					if now.Sub(t) < rl.window {
						fresh = append(fresh, t)
					}
				}
				if len(fresh) == 0 {
					delete(rl.attempts, ip)
				} else {
					rl.attempts[ip] = fresh
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	times := rl.attempts[ip]
	var fresh []time.Time
	for _, t := range times {
		if now.Sub(t) < rl.window {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) >= rl.maxHits {
		rl.attempts[ip] = fresh
		return false
	}
	rl.attempts[ip] = append(fresh, now)
	return true
}

// TenantHandler manages operator (tenant) lifecycle.
type TenantHandler struct {
	repo       *tenant.Repo
	jwtSecret  string
	jwtIssuer  string
	jwtTTL     int // days
	loginRL    *ipRateLimiter // 10 login attempts per IP per 5 minutes
	registerRL *ipRateLimiter // 3 registrations per IP per 10 minutes
}

// NewTenantHandler wires the handler with a repo and JWT config.
func NewTenantHandler(repo *tenant.Repo, secret, issuer string, ttlDays int) *TenantHandler {
	return &TenantHandler{
		repo:       repo,
		jwtSecret:  secret,
		jwtIssuer:  issuer,
		jwtTTL:     ttlDays,
		loginRL:    newIPRateLimiter(5*time.Minute, 10),
		registerRL: newIPRateLimiter(10*time.Minute, 3),
	}
}

// RegisterPublic binds unauthenticated endpoints (onboarding + bootstrap).
func (h *TenantHandler) RegisterPublic(r fiber.Router) {
	r.Post("/onboard/register", h.register)
	r.Post("/onboard/login", h.login)
	r.Get("/public/:code/bootstrap", h.bootstrap)
	r.Get("/public/:code/brand", h.brand)
}

// RegisterProtected binds JWT-protected operator endpoints.
func (h *TenantHandler) RegisterProtected(r fiber.Router) {
	r.Get("/me", h.getMe)
	r.Get("/me/lines", h.listLines)
	r.Post("/me/lines", h.createLine)
	r.Put("/me/lines/:id", h.updateLine)
	r.Delete("/me/lines/:id", h.deleteLine)
	r.Get("/me/theme", h.getTheme)
	r.Put("/me/theme", h.updateTheme)
	r.Get("/me/codes", h.listCodes)
	r.Post("/me/codes", h.createCode)
	r.Delete("/me/codes/:code", h.deleteCode)
	r.Get("/me/agents", h.listAgents)
	r.Get("/me/stats", h.getStats)
	r.Put("/me/password", h.changePassword)
	r.Post("/me/agents/:id/rotate-key", h.rotateAgentKey)
}

// ---- Public: Operator Registration ----

type registerReq struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *TenantHandler) register(c *fiber.Ctx) error {
	if !h.registerRL.allow(c.IP()) {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many registration attempts, try again later")
	}
	var req registerReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if req.Name == "" || req.Slug == "" || req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name, slug, email, password required")
	}
	if len(req.Password) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "hash error")
	}

	t := &tenant.Tenant{
		Name:       req.Name,
		Slug:       req.Slug,
		Email:      req.Email,
		AdminHash:  string(hash),
		Plan:       tenant.PlanFree,
		MaxPlayers: 50,
		MaxLines:   1,
	}

	id, err := h.repo.CreateTenant(c.Context(), t)
	if err != nil {
		return fiber.NewError(fiber.StatusConflict, "registration failed: "+err.Error())
	}

	// Auto-create a default invite code (uppercase slug)
	code := slugToCode(req.Slug)
	_ = h.repo.CreateTenantCode(c.Context(), code, id)

	// Issue JWT
	token, err := h.issueJWT(id, req.Email)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "token error")
	}

	return c.JSON(fiber.Map{
		"ok":        true,
		"tenant_id": id,
		"code":      code,
		"token":     token,
	})
}

// ---- Public: Operator Login ----

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *TenantHandler) login(c *fiber.Ctx) error {
	if !h.loginRL.allow(c.IP()) {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many login attempts, try again later")
	}
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}

	t, err := h.repo.GetTenantByEmail(c.Context(), req.Email)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(t.AdminHash), []byte(req.Password)); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	}

	// Block suspended tenants
	if t.Suspended {
		return fiber.NewError(fiber.StatusForbidden, "account suspended — contact support")
	}

	token, err := h.issueJWT(t.ID, t.Email)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "token error")
	}

	return c.JSON(fiber.Map{"ok": true, "token": token, "tenant_id": t.ID})
}

// ---- Public: Launcher Bootstrap ----

func (h *TenantHandler) bootstrap(c *fiber.Ctx) error {
	code := c.Params("code")
	tenantID, err := h.repo.ResolveTenantCode(c.Context(), code)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "unknown server code")
	}

	t, err := h.repo.GetTenant(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if t.Suspended {
		return fiber.NewError(fiber.StatusForbidden, "server suspended")
	}

	theme, _ := h.repo.GetTheme(c.Context(), tenantID)
	lines, _ := h.repo.ListServerLines(c.Context(), tenantID)

	// Find gate agent's public IP (most recently seen online agent)
	gateIP := h.repo.GetOnlineGateIP(c.Context(), tenantID)

	resp := tenant.LauncherBootstrap{
		TenantID:   tenantID,
		TenantName: t.Name,
		GateIP:     gateIP,
	}
	if theme != nil {
		resp.Theme = *theme
	}
	resp.ServerLines = lines

	return c.JSON(resp)
}

// ---- Public: Brand (subset of bootstrap, just theme) ----

func (h *TenantHandler) brand(c *fiber.Ctx) error {
	code := c.Params("code")
	tenantID, err := h.repo.ResolveTenantCode(c.Context(), code)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "unknown server code")
	}
	theme, err := h.repo.GetTheme(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "no theme configured")
	}
	return c.JSON(theme)
}

// ---- Protected: Operator Dashboard ----

func (h *TenantHandler) getMe(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	t, err := h.repo.GetTenant(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(t)
}

func (h *TenantHandler) listLines(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	lines, err := h.repo.ListServerLines(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(lines)
}

type createLineReq struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	AuthPort int    `json:"auth_port"`
	GamePort int    `json:"game_port"`
	ChatPort int    `json:"chat_port"`
	GameArgs string `json:"game_args"`
}

func (h *TenantHandler) createLine(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	var req createLineReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	line := &tenant.ServerLine{
		TenantID: tenantID,
		Name:     req.Name,
		Version:  req.Version,
		AuthPort: req.AuthPort,
		GamePort: req.GamePort,
		ChatPort: req.ChatPort,
		GameArgs: req.GameArgs,
	}
	id, err := h.repo.CreateServerLine(c.Context(), line)
	if err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "id": id})
}

type updateLineReq struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	AuthPort int    `json:"auth_port"`
	GamePort int    `json:"game_port"`
	ChatPort int    `json:"chat_port"`
}

func (h *TenantHandler) updateLine(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	lineID := c.Params("id")
	var req updateLineReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if req.Name == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name required")
	}
	if err := h.repo.UpdateServerLine(c.Context(), tenantID, lineID,
		req.Name, req.Version, req.AuthPort, req.GamePort, req.ChatPort); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *TenantHandler) deleteLine(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	lineID := c.Params("id")
	if err := h.repo.DeleteServerLine(c.Context(), tenantID, lineID); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *TenantHandler) getTheme(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	theme, err := h.repo.GetTheme(c.Context(), tenantID)
	if err != nil {
		// No theme yet is normal — return empty object
		return c.JSON(fiber.Map{})
	}
	return c.JSON(theme)
}

func (h *TenantHandler) updateTheme(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	var theme tenant.LauncherTheme
	if err := c.BodyParser(&theme); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	theme.TenantID = tenantID
	if err := h.repo.UpsertTheme(c.Context(), &theme); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

type createCodeReq struct {
	Code string `json:"code"`
}

func (h *TenantHandler) createCode(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	var req createCodeReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if err := h.repo.CreateTenantCode(c.Context(), slugToCode(req.Code), tenantID); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *TenantHandler) deleteCode(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	code := c.Params("code")
	if err := h.repo.DeleteTenantCode(c.Context(), tenantID, code); err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *TenantHandler) listAgents(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	agents, err := h.repo.ListGateAgents(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if agents == nil {
		agents = []tenant.GateAgent{}
	}
	return c.JSON(agents)
}

func (h *TenantHandler) listCodes(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	codes, err := h.repo.ListTenantCodes(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if codes == nil {
		codes = []tenant.TenantCode{}
	}
	return c.JSON(codes)
}

// ---- Password Change ----

type changePasswordReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *TenantHandler) changePassword(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	var req changePasswordReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		return fiber.NewError(fiber.StatusBadRequest, "old_password and new_password required")
	}
	if len(req.NewPassword) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "new password must be at least 8 characters")
	}

	// Verify old password
	t, err := h.repo.GetTenant(c.Context(), tenantID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	if err := bcrypt.CompareHashAndPassword([]byte(t.AdminHash), []byte(req.OldPassword)); err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "current password incorrect")
	}

	// Hash and store new password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "hash error")
	}
	if err := h.repo.UpdateTenantPassword(c.Context(), tenantID, string(hash)); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{"ok": true})
}

// ---- Stats API ----

func (h *TenantHandler) getStats(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	days := c.QueryInt("days", 7)
	stats, err := h.repo.ListDailyStats(c.Context(), tenantID, days)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if stats == nil {
		stats = []tenant.DailyStat{}
	}
	return c.JSON(stats)
}

// ---- Agent Key Rotation ----

func (h *TenantHandler) rotateAgentKey(c *fiber.Ctx) error {
	tenantID := c.Locals("tenant_id").(string)
	agentID := c.Params("id")
	newKey, err := h.repo.RotateAgentKey(c.Context(), tenantID, agentID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "new_key": newKey})
}

// ---- helpers ----

func (h *TenantHandler) issueJWT(tenantID, email string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       tenantID,
		"email":     email,
		"tenant_id": tenantID,
		"iss":       h.jwtIssuer,
		"iat":       now.Unix(),
		"exp":       now.Add(time.Duration(h.jwtTTL) * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.jwtSecret))
}

func slugToCode(slug string) string {
	// Convert slug to uppercase invite code (e.g. "juezhan-yh" → "JUEZHAN-YH")
	result := make([]byte, 0, len(slug))
	for _, b := range []byte(slug) {
		if b >= 'a' && b <= 'z' {
			result = append(result, b-32)
		} else if (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '-' {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		return "DEFAULT"
	}
	return string(result)
}
