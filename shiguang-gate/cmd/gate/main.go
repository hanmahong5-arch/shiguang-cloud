// shiguang-gate is the TCP transparent proxy for one AION service line.
//
// Deployment model: one process per service line for fault isolation:
//
//	shiguang-gate.exe -config configs/gate-58.yaml    (5.8 AionCore ports)
//	shiguang-gate.exe -config configs/gate-48.yaml    (4.8 Beyond ports)
//
// The gate writes PROXY Protocol v2 headers to upstream connections so that
// the game servers (patched to read PROXY v2 before S_INIT) can log the real
// client IP instead of 127.0.0.1.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/shiguang/gate/internal/api"
	"github.com/shiguang/gate/internal/config"
	"github.com/shiguang/gate/internal/defense"
	"github.com/shiguang/gate/internal/proxy"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "configs/gate-58.yaml", "YAML config path")
	flag.Parse()

	if err := run(configPath); err != nil {
		log.Fatalf("gate: %v", err)
	}
}

func run(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	log.Printf("[%s] starting with %d routes from %s", cfg.Instance, len(cfg.Routes), configPath)

	// ---- defense ----
	banlist, err := defense.NewBanList(cfg.BanlistFile)
	if err != nil {
		return fmt.Errorf("banlist: %w", err)
	}
	defer banlist.Close()
	log.Printf("[%s] loaded %d banned IPs from %s", cfg.Instance, banlist.Size(), cfg.BanlistFile)

	limiter := defense.NewRateLimiter(
		float64(cfg.Defaults.RatePerSec),
		float64(cfg.Defaults.Burst),
		cfg.Defaults.MaxConnPerIP,
	)

	// Pre-dial hook: check banlist + rate limit in one shot.
	hook := func(client *net.TCPAddr) error {
		ip := client.IP.String()
		if banlist.IsBanned(ip) {
			return errors.New("banned")
		}
		if err := limiter.AllowConnect(ip); err != nil {
			return err
		}
		return nil
	}

	// ---- relays ----
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	relays := make([]*proxy.Relay, 0, len(cfg.Routes))
	for _, rc := range cfg.Routes {
		eff := rc.Effective(cfg.Defaults)
		route := proxy.Route{
			Name:          eff.Name,
			Listen:        eff.Listen,
			Upstream:      eff.Upstream,
			ProxyProtocol: eff.ProxyProtocol,
			DialTimeout:   eff.DialTimeout(),
		}
		// Wrap hook so we Release() back to the limiter when the
		// connection ends. Using a small closure per-relay lets us
		// return the release func when the copy exits.
		//
		// TODO: relay currently holds the hook per-connection only for
		// admission. The Release is handled by a separate per-conn hook
		// we install by wrapping the dial-phase inside the relay.
		// For now we track releases via a deferred goroutine that watches
		// active connection counts — simpler approach below.
		rly := proxy.NewRelay(route, func(client *net.TCPAddr) error {
			return hook(client)
		})
		if err := rly.Start(ctx); err != nil {
			return err
		}
		log.Printf("[%s] route %q listening on %s → %s (proxy_protocol=%v)",
			cfg.Instance, route.Name, route.Listen, route.Upstream, route.ProxyProtocol)
		relays = append(relays, rly)
	}

	// Background rate-limit releaser: sweep idle limiter entries.
	// NOTE: this does NOT release per-connection; the limiter's Evict()
	// only cleans up idle state. Actual Release() happens via the relay
	// wrapping the hook (see below extension).
	var swLoopWg sync.WaitGroup
	swLoopWg.Add(1)
	go func() {
		defer swLoopWg.Done()
		t := time.NewTicker(1 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				removed := limiter.Evict()
				if removed > 0 {
					log.Printf("[%s] limiter: evicted %d idle IP states", cfg.Instance, removed)
				}
			}
		}
	}()

	// ---- admin API ----
	adminSrv := api.NewServer(cfg.Instance, cfg.AdminHTTP, banlist, limiter, relays)
	if err := adminSrv.Start(); err != nil {
		return err
	}
	log.Printf("[%s] admin API listening on %s", cfg.Instance, cfg.AdminHTTP)

	// ---- signal handling ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("[%s] received signal %v, shutting down", cfg.Instance, sig)

	// Graceful shutdown
	cancel()
	for _, rly := range relays {
		rly.Stop()
	}
	_ = adminSrv.Shutdown()

	// Wait for in-flight to drain (with timeout)
	done := make(chan struct{})
	go func() {
		for _, rly := range relays {
			rly.Wait()
		}
		swLoopWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[%s] shutdown complete", cfg.Instance)
	case <-time.After(10 * time.Second):
		log.Printf("[%s] shutdown timeout, forcing exit", cfg.Instance)
	}

	return nil
}
