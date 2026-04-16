// Package handlers contains Fiber HTTP handlers for shiguang-control.
package handlers

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/shiguang/control/internal/httputil"
	"github.com/shiguang/control/internal/metrics"
	"github.com/shiguang/control/internal/service"
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

// allow returns true if the IP is within rate limits.
func (rl *ipRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()

	// Prune old entries for this IP
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

// AccountHandler routes account operations to the correct line service
// (5.8 or 4.8) based on the request's "server" field.
type AccountHandler struct {
	svc58      service.AccountService
	svc48      service.AccountService
	tokens     *service.TokenStore      // session token handoff
	registerRL *ipRateLimiter           // 5 registrations per IP per 10 minutes
	loginRL    *ipRateLimiter           // 20 login attempts per IP per 5 minutes
	extAuthRL  *ipRateLimiter           // 60 external auth attempts per IP per minute
}

// NewAccountHandler wires the two line-specific services.
func NewAccountHandler(svc58, svc48 service.AccountService) *AccountHandler {
	return &AccountHandler{
		svc58:      svc58,
		svc48:      svc48,
		tokens:     service.NewTokenStore(),
		registerRL: newIPRateLimiter(10*time.Minute, 5),
		loginRL:    newIPRateLimiter(5*time.Minute, 20),
		extAuthRL:  newIPRateLimiter(1*time.Minute, 60),
	}
}

// Register binds account + token + external-auth endpoints.
func (h *AccountHandler) Register(r fiber.Router) {
	g := r.Group("/account")
	g.Post("/register", h.register)
	g.Post("/login", h.login)
	g.Post("/change_password", h.changePassword)
	g.Post("/reset_password", h.resetPassword)

	// Token validation endpoint — called by game server's auth handler
	// on the same machine (localhost). No JWT required; network-level trust.
	t := r.Group("/token")
	t.Post("/validate", h.validateToken)

	// ExternalAuth bridge — compatible with Beyond 4.8's ExternalAuth.java.
	// Set loginserver.accounts.external_auth.url to point here.
	// Receives: {"user": "name", "password": "password"}
	// Returns:  {"accountId": "name", "aionAuthResponseId": 0}
	r.Post("/external-auth", h.externalAuth)
}

type accountReq struct {
	Server   string `json:"server"` // "5.8" or "4.8"
	Name     string `json:"name"`
	Password string `json:"password"`
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password,omitempty"`
	Email    string `json:"email,omitempty"`
}

func (h *AccountHandler) pick(server string) (service.AccountService, error) {
	switch server {
	case "5.8", "58":
		if h.svc58 == nil {
			return nil, errors.New("5.8 service not configured")
		}
		return h.svc58, nil
	case "4.8", "48":
		if h.svc48 == nil {
			return nil, errors.New("4.8 service not configured")
		}
		return h.svc48, nil
	}
	return nil, errors.New("unknown server line: must be '5.8' or '4.8'")
}

func (h *AccountHandler) register(c *fiber.Ctx) error {
	// Rate limit: 5 registrations per IP per 10 minutes
	if !h.registerRL.allow(c.IP()) {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many registration attempts, try again later")
	}
	var req accountReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON: "+err.Error())
	}
	svc, err := h.pick(req.Server)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := svc.Register(c.Context(), req.Name, req.Password, req.Email); err != nil {
		metrics.RegistersFailed.Add(1)
		return translateErr(err)
	}
	metrics.RegistersOK.Add(1)
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AccountHandler) login(c *fiber.Ctx) error {
	// Rate limit: 20 login attempts per IP per 5 minutes
	if !h.loginRL.allow(c.IP()) {
		return fiber.NewError(fiber.StatusTooManyRequests, "too many login attempts, try again later")
	}
	var req accountReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	svc, err := h.pick(req.Server)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := svc.Login(c.Context(), req.Name, req.Password); err != nil {
		metrics.LoginsFailed.Add(1)
		return translateErr(err)
	}

	// Issue a session token for the Token Handoff flow
	metrics.LoginsOK.Add(1)
	token, err := h.tokens.Issue(req.Name, req.Server)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "token generation failed")
	}
	metrics.TokensIssued.Add(1)
	log.Printf("[token-handoff] issued account=%s server=%s ip=%s", req.Name, req.Server, c.IP())

	return c.JSON(fiber.Map{"ok": true, "session_token": token})
}

func (h *AccountHandler) changePassword(c *fiber.Ctx) error {
	var req accountReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	svc, err := h.pick(req.Server)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if err := svc.ChangePassword(c.Context(), req.Name, req.OldPassword, req.NewPassword); err != nil {
		return translateErr(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AccountHandler) resetPassword(c *fiber.Ctx) error {
	var req accountReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON")
	}
	svc, err := h.pick(req.Server)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	newPw, err := svc.ResetPassword(c.Context(), req.Name, req.Email)
	if err != nil {
		return translateErr(err)
	}
	return c.JSON(fiber.Map{"ok": true, "new_password": newPw})
}

// isLoopbackIP reports whether ip is a loopback address. Accepts IPv4 (127/8)
// and IPv6 (::1) forms. Empty / unparseable strings return false so that any
// ambiguity yields denial rather than accidental exposure.
func isLoopbackIP(ip string) bool {
	if ip == "" {
		return false
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}

// validateToken consumes a session token and returns the account.
// Called by the game server's auth handler: POST /api/token/validate
// Body: {"token": "..."}
// Returns: {"ok": true, "account": "...", "server": "5.8"}
//
// SECURITY: This endpoint is trusted by the login-server as an unauthenticated
// oracle — any caller who hits it with a valid token can consume it. Therefore
// we HARD-REQUIRE the caller be on loopback. Production deployments where the
// LS runs on a different host must either (a) colocate control + LS on the
// same box, or (b) front this endpoint with mTLS and drop the loopback guard.
func (h *AccountHandler) validateToken(c *fiber.Ctx) error {
	// Reject non-loopback callers — audit log + 403 so operators see the leak.
	if !isLoopbackIP(c.IP()) {
		metrics.TokensBlocked.Add(1)
		log.Printf("[token-handoff] reject_non_loopback ip=%s path=%s", c.IP(), c.Path())
		return httputil.Forbidden(c, "token validation restricted to loopback")
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := c.BodyParser(&req); err != nil {
		return httputil.BadRequest(c, "invalid JSON: "+err.Error())
	}
	if req.Token == "" {
		return httputil.BadRequest(c, "token required")
	}
	// Defensive length bounds — 12-byte hex tokens from TokenStore are 24 chars.
	// Reject anything absurd to prevent pathological Consume() calls.
	if len(req.Token) < 8 || len(req.Token) > 128 {
		return httputil.BadRequest(c, "token length out of range [8,128]")
	}

	account, server, err := h.tokens.Consume(req.Token)
	if err != nil {
		metrics.TokensRejected.Add(1)
		return httputil.Unauthorized(c, err.Error())
	}
	metrics.TokensConsumed.Add(1)

	return c.JSON(fiber.Map{"ok": true, "account": account, "server": server})
}

// externalAuth handles Beyond 4.8's ExternalAuth.java requests.
// This provides a drop-in replacement: the login server sends the player's
// credentials here, and we validate against Control's account database.
//
// Two modes:
//  1. Normal login: {"user":"name","password":"password"} → validate against DB
//  2. Token Handoff: {"user":"name","password":"SG-{token}"} → validate session token
//
// Response format matches ExternalAuth.Response record:
//   {"accountId":"name","aionAuthResponseId":0}  (0 = success)
//   {"accountId":"",    "aionAuthResponseId":3}  (3 = wrong password)
func (h *AccountHandler) externalAuth(c *fiber.Ctx) error {
	// Rate limit: 60 requests per IP per minute (game server auth traffic)
	if !h.extAuthRL.allow(c.IP()) {
		log.Printf("[external-auth] rate_limited ip=%s", c.IP())
		return c.Status(429).JSON(fiber.Map{
			"accountId":          "",
			"aionAuthResponseId": 20,
		})
	}

	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"accountId":          "",
			"aionAuthResponseId": 20, // SYSTEM_ERROR
		})
	}

	// Input validation — reject obviously malformed requests
	if len(req.User) > 64 || len(req.Password) > 128 {
		return c.Status(400).JSON(fiber.Map{
			"accountId":          "",
			"aionAuthResponseId": 20,
		})
	}

	// Mode 2: Token Handoff — password starts with "SG-"
	if len(req.Password) > 3 && req.Password[:3] == "SG-" {
		token := req.Password[3:]
		account, server, err := h.tokens.Consume(token)
		if err != nil {
			log.Printf("[token-handoff] reject user=%s reason=%v ip=%s", req.User, err, c.IP())
			return c.JSON(fiber.Map{
				"accountId":          "",
				"aionAuthResponseId": 3, // INCORRECT_PWD (token invalid/expired)
			})
		}
		log.Printf("[token-handoff] accept user=%s account=%s server=%s ip=%s", req.User, account, server, c.IP())
		return c.JSON(fiber.Map{
			"accountId":          account,
			"aionAuthResponseId": 0, // ALL_OK
		})
	}

	// Mode 1: Normal credential validation — try both service lines.
	// ExternalAuth callers don't specify which line, so we try all available.
	if h.svc48 == nil && h.svc58 == nil {
		return c.Status(503).JSON(fiber.Map{
			"accountId":          "",
			"aionAuthResponseId": 62, // ACCOUNTCACHESERVER_DOWN
		})
	}

	// Try each configured service in order; first successful match wins.
	services := []service.AccountService{h.svc48, h.svc58}
	for _, svc := range services {
		if svc == nil {
			continue
		}
		err := svc.Login(c.Context(), req.User, req.Password)
		if err == nil {
			metrics.ExternalAuthOK.Add(1)
			log.Printf("[external-auth] login_ok user=%s ip=%s", req.User, c.IP())
			return c.JSON(fiber.Map{
				"accountId":          req.User,
				"aionAuthResponseId": 0, // ALL_OK
			})
		}
		// If account not found on this line, try next line
		if errors.Is(err, service.ErrAccountNotFound) {
			continue
		}
		// Bad credentials means account exists but wrong password — stop trying
		log.Printf("[external-auth] login_fail user=%s reason=%v ip=%s", req.User, err, c.IP())
		return c.JSON(fiber.Map{
			"accountId":          "",
			"aionAuthResponseId": 3, // INCORRECT_PWD
		})
	}

	// Account not found on any line
	metrics.ExternalAuthFailed.Add(1)
	log.Printf("[external-auth] not_found user=%s ip=%s", req.User, c.IP())
	return c.JSON(fiber.Map{
		"accountId":          "",
		"aionAuthResponseId": 4, // ACCOUNT_LOAD_FAIL
	})
}

// translateErr maps service sentinel errors to HTTP statuses.
func translateErr(err error) error {
	switch {
	case errors.Is(err, service.ErrAccountExists):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case errors.Is(err, service.ErrBadCredentials):
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrAccountNotFound):
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrEmailMismatch):
		return fiber.NewError(fiber.StatusForbidden, err.Error())
	}
	return fiber.NewError(fiber.StatusInternalServerError, err.Error())
}
