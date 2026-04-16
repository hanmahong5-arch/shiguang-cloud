// Package embed provides a façade for embedding the gate subsystem into the
// unified shiguang-agent binary. It wraps internal packages (proxy, defense,
// api) behind a clean Start/Stop lifecycle interface.
//
// This package is NOT in internal/ so that shiguang-agent can import it
// across Go module boundaries, while the actual implementation stays in
// internal/ for proper encapsulation.
//
// Usage from shiguang-agent:
//
//	import gateEmbed "github.com/shiguang/gate/pkg/embed"
//	g, err := gateEmbed.Start(ctx, cfg)
//	defer g.Stop()
package embed

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/shiguang/gate/internal/api"
	"github.com/shiguang/gate/internal/defense"
	"github.com/shiguang/gate/internal/proxy"
)

// GateConfig is the configuration for the embedded gate subsystem.
// The agent's main.go converts its AgentConfig.Gate section into this.
type GateConfig struct {
	Instance    string        // human-readable name for logs (e.g. "agent-gate")
	BanlistFile string        // path to ban persistence JSON
	AdminBind   string        // HTTP admin API bind (e.g. "127.0.0.1:9090")
	Routes      []RouteConfig // TCP relay routes
	Defaults    DefenseDefaults
}

// RouteConfig mirrors proxy.Route but without internal type exposure.
type RouteConfig struct {
	Name          string
	Listen        string
	Upstream      string
	ProxyProtocol bool
	DialTimeoutMS int
}

// DefenseDefaults holds rate limiting and connection cap parameters.
type DefenseDefaults struct {
	MaxConnPerIP int
	RatePerSec   int
	Burst        int
}

// GateInstance is a running gate subsystem. Call Stop() to shut down.
type GateInstance struct {
	relays   []*proxy.Relay
	banlist  *defense.BanList
	limiter  *defense.RateLimiter
	adminSrv *api.Server
	ctx      context.Context
	cancel   context.CancelFunc
}

// Start initializes and starts the gate subsystem (TCP relays + defense + admin API).
// It blocks briefly during setup, then returns. Actual relay loops run in goroutines.
// The caller should select on ctx.Done() and call Stop() for graceful shutdown.
func Start(parentCtx context.Context, cfg GateConfig) (*GateInstance, error) {
	ctx, cancel := context.WithCancel(parentCtx)

	// Defense layer
	banlist, err := defense.NewBanList(cfg.BanlistFile)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("banlist: %w", err)
	}
	log.Printf("[gate] loaded %d banned IPs from %s", banlist.Size(), cfg.BanlistFile)

	rate := float64(cfg.Defaults.RatePerSec)
	if rate <= 0 {
		rate = 5
	}
	burst := float64(cfg.Defaults.Burst)
	if burst <= 0 {
		burst = 10
	}
	limiter := defense.NewRateLimiter(rate, burst, cfg.Defaults.MaxConnPerIP)

	// Pre-dial hook: banlist + rate limit check
	hook := func(client *net.TCPAddr) error {
		ip := client.IP.String()
		if banlist.IsBanned(ip) {
			return errors.New("banned")
		}
		return limiter.AllowConnect(ip)
	}

	// Start relays
	relays := make([]*proxy.Relay, 0, len(cfg.Routes))
	for _, rc := range cfg.Routes {
		dialTimeout := 5 * time.Second
		if rc.DialTimeoutMS > 0 {
			dialTimeout = time.Duration(rc.DialTimeoutMS) * time.Millisecond
		}
		route := proxy.Route{
			Name:          rc.Name,
			Listen:        rc.Listen,
			Upstream:      rc.Upstream,
			ProxyProtocol: rc.ProxyProtocol,
			DialTimeout:   dialTimeout,
		}
		rly := proxy.NewRelay(route, hook)
		if err := rly.Start(ctx); err != nil {
			// Clean up already-started relays
			for _, prev := range relays {
				prev.Stop()
			}
			banlist.Close()
			cancel()
			return nil, fmt.Errorf("relay %s: %w", rc.Name, err)
		}
		log.Printf("[gate] route %q listening on %s → %s (proxy_v2=%v)",
			rc.Name, rc.Listen, rc.Upstream, rc.ProxyProtocol)
		relays = append(relays, rly)
	}

	// Admin HTTP API
	var adminSrv *api.Server
	if cfg.AdminBind != "" {
		instance := cfg.Instance
		if instance == "" {
			instance = "gate"
		}
		adminSrv = api.NewServer(instance, cfg.AdminBind, banlist, limiter, relays)
		if err := adminSrv.Start(); err != nil {
			log.Printf("[gate] admin API failed to start: %v", err)
			// Non-fatal: gate still functions without admin API
		} else {
			log.Printf("[gate] admin API on %s", cfg.AdminBind)
		}
	}

	return &GateInstance{
		relays:   relays,
		banlist:  banlist,
		limiter:  limiter,
		adminSrv: adminSrv,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Stop gracefully shuts down all relays, the admin API, and persists the banlist.
func (g *GateInstance) Stop() {
	g.cancel()
	for _, rly := range g.relays {
		rly.Stop()
	}
	if g.adminSrv != nil {
		_ = g.adminSrv.Shutdown()
	}
	_ = g.banlist.Close()
	// Wait for relay goroutines to drain
	for _, rly := range g.relays {
		rly.Wait()
	}
	log.Printf("[gate] stopped")
}

// Banlist returns the defense banlist for external command injection (Hub → ban).
func (g *GateInstance) Banlist() *defense.BanList { return g.banlist }

// RelayStats returns a snapshot of all relay counters for heartbeat reporting.
func (g *GateInstance) RelayStats() []RelaySnapshot {
	out := make([]RelaySnapshot, len(g.relays))
	for i, rly := range g.relays {
		acc, rej, act := rly.Stats()
		out[i] = RelaySnapshot{
			Name:            rly.Name(),
			Active:          act,
			Accepted:        acc,
			Rejected:        rej,
			UpstreamHealthy: rly.UpstreamHealthy(),
		}
	}
	return out
}

// RelaySnapshot is a public-safe snapshot of relay counters.
type RelaySnapshot struct {
	Name            string
	Active          int64
	Accepted        uint64
	Rejected        uint64
	UpstreamHealthy bool
}
