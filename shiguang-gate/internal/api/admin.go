// Package api provides the HTTP admin endpoints for shiguang-gate.
// These endpoints are called by shiguang-control (the orchestrator) to ban IPs,
// query status, and drive operational commands.
//
// Security: this API is bound to 127.0.0.1 by default. Exposing it publicly
// REQUIRES an authentication layer — adding one is left to control's reverse
// proxy (it adds a Bearer token and speaks to gate over localhost only).
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/shiguang/gate/internal/defense"
	"github.com/shiguang/gate/internal/proxy"
)

// Server is the admin HTTP server bound to config.admin_http.
type Server struct {
	instance string
	banlist  *defense.BanList
	limiter  *defense.RateLimiter
	relays   []*proxy.Relay

	srv *http.Server
}

// NewServer constructs an admin HTTP server. relays is the list of active
// relay instances whose stats are exposed via /status.
func NewServer(instance, bind string, banlist *defense.BanList, limiter *defense.RateLimiter, relays []*proxy.Relay) *Server {
	mux := http.NewServeMux()
	s := &Server{
		instance: instance,
		banlist:  banlist,
		limiter:  limiter,
		relays:   relays,
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/ban", s.handleBan)
	mux.HandleFunc("/unban", s.handleUnban)
	mux.HandleFunc("/banlist", s.handleBanlist)

	s.srv = &http.Server{
		Addr:              bind,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return s
}

// Start launches the HTTP server in a goroutine and returns immediately.
// Any listen error is logged but does not prevent the gate from running.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return fmt.Errorf("admin api listen %s: %w", s.srv.Addr, err)
	}
	go func() {
		_ = s.srv.Serve(ln)
	}()
	return nil
}

// Shutdown gracefully stops the server, draining in-flight requests
// within a 5-second deadline before forcing close.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// ---- handlers ----

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// Check if any upstream is healthy — if all dead, report degraded
	allUnhealthy := len(s.relays) > 0
	var totalActive int64
	for _, relay := range s.relays {
		if relay.UpstreamHealthy() {
			allUnhealthy = false
		}
		_, _, act := relay.Stats()
		totalActive += act
	}

	status := http.StatusOK
	health := "healthy"
	if allUnhealthy && len(s.relays) > 0 {
		status = http.StatusServiceUnavailable
		health = "degraded"
	}

	writeJSON(w, status, map[string]any{
		"status":      health,
		"routes":      len(s.relays),
		"active_conn": totalActive,
		"ban_count":   s.banlist.Size(),
	})
}

type statusResponse struct {
	Instance string        `json:"instance"`
	Uptime   string        `json:"uptime"`
	Routes   []routeStatus `json:"routes"`
	BanCount int           `json:"ban_count"`
	LiveIPs  int           `json:"live_ips"`
}

type routeStatus struct {
	Name            string `json:"name"`
	Accepted        uint64 `json:"accepted"`
	Rejected        uint64 `json:"rejected"`
	Active          int64  `json:"active"`
	UpstreamHealthy bool   `json:"upstream_healthy"`
}

var startTime = time.Now()

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		Instance: s.instance,
		Uptime:   time.Since(startTime).Round(time.Second).String(),
		BanCount: s.banlist.Size(),
		LiveIPs:  s.limiter.Size(),
	}
	for _, relay := range s.relays {
		acc, rej, act := relay.Stats()
		resp.Routes = append(resp.Routes, routeStatus{
			Name:            relay.Name(),
			Accepted:        acc,
			Rejected:        rej,
			Active:          act,
			UpstreamHealthy: relay.UpstreamHealthy(),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type banRequest struct {
	IP         string `json:"ip"`
	Reason     string `json:"reason"`
	DurationMS int64  `json:"duration_ms,omitempty"` // 0 = permanent
}

func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req banRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.IP == "" {
		http.Error(w, "ip required", http.StatusBadRequest)
		return
	}
	duration := time.Duration(req.DurationMS) * time.Millisecond
	s.banlist.Ban(req.IP, req.Reason, duration)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type unbanRequest struct {
	IP string `json:"ip"`
}

func (s *Server) handleUnban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req unbanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.IP == "" {
		http.Error(w, "ip required", http.StatusBadRequest)
		return
	}
	removed := s.banlist.Unban(req.IP)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "existed": removed})
}

func (s *Server) handleBanlist(w http.ResponseWriter, r *http.Request) {
	list := s.banlist.List()
	writeJSON(w, http.StatusOK, list)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
