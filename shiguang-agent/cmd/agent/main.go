// shiguang-agent is the unified spoke binary that combines gate (TCP relay)
// and control (account API + WSS hub) into a single process.
//
// Deployment: the tenant (server operator) downloads one binary + one YAML.
//
//   shiguang-agent.exe -config agent.yaml
//
// Lifecycle: all subsystems are managed via errgroup.Group with a shared
// context. If any subsystem fails fatally, the context is cancelled and
// all others shut down gracefully. This prevents zombie goroutines.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"

	ctrlEmbed "github.com/shiguang/control/pkg/embed"
	gateEmbed "github.com/shiguang/gate/pkg/embed"

	"github.com/shiguang/agent/internal/hubconn"
	pb "github.com/shiguang/shared/hubpb"
	"github.com/shiguang/shared/sglog"
	"github.com/shiguang/shared/tenant"
)

// AgentConfig merges what was previously gate + control configs into one YAML.
type AgentConfig struct {
	TenantSlug string `yaml:"tenant_slug"`
	AgentToken string `yaml:"agent_token"`
	HubAddress string `yaml:"hub_address"`

	Gate struct {
		BanlistFile string       `yaml:"banlist_file"`
		AdminBind   string       `yaml:"admin_bind"`
		Routes      []RouteEntry `yaml:"routes"`
		Defaults    struct {
			MaxConnPerIP  int `yaml:"max_conn_per_ip"`
			RatePerSec    int `yaml:"rate_per_sec"`
			Burst         int `yaml:"burst"`
			DialTimeoutMS int `yaml:"dial_timeout_ms"`
		} `yaml:"defaults"`
	} `yaml:"gate"`

	Control struct {
		Bind      string `yaml:"bind"`
		JWTSecret string `yaml:"jwt_secret"`
		DB58      string `yaml:"db_58"`
		DB48      string `yaml:"db_48"`
		AdminUser string `yaml:"admin_user"`
		AdminPass string `yaml:"admin_pass"`
		WebDir    string `yaml:"web_dir"`
	} `yaml:"control"`
}

type RouteEntry struct {
	Name          string `yaml:"name"`
	Listen        string `yaml:"listen"`
	Upstream      string `yaml:"upstream"`
	ProxyProtocol bool   `yaml:"proxy_protocol"`
	DialTimeoutMS int    `yaml:"dial_timeout_ms"`
}

func main() {
	var configPath string
	var dev bool
	flag.StringVar(&configPath, "config", "agent.yaml", "agent config path")
	flag.BoolVar(&dev, "dev", false, "development mode (human-readable logs)")
	flag.Parse()

	sglog.Init("agent", dev)

	cfg, err := loadConfig(configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	slog.Info("starting",
		"tenant", cfg.TenantSlug,
		"hub", cfg.HubAddress,
		"routes", len(cfg.Gate.Routes),
		"control_bind", cfg.Control.Bind)

	// Root context: cancelled on SIGINT/SIGTERM or any subsystem fatal error.
	rootCtx, rootCancel := context.WithCancel(context.Background())

	// Signal handler in a goroutine — cancels root context on signal.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		s := <-sig
		slog.Info("shutdown signal received", "signal", s.String())
		rootCancel()
	}()

	// errgroup: all subsystems share the same context.
	// If any returns a non-nil error, the group cancels ctx for the rest.
	g, ctx := errgroup.WithContext(rootCtx)

	// Gate instance is shared between subsystems (heartbeat needs relay stats).
	var gate *gateEmbed.GateInstance
	var gateReady = make(chan struct{})

	// ── Subsystem 1: Gate (TCP relays + defense + admin API) ──
	g.Go(func() error {
		gateCfg := toGateConfig(cfg)
		var err error
		gate, err = gateEmbed.Start(ctx, gateCfg)
		if err != nil {
			close(gateReady)
			return fmt.Errorf("gate start: %w", err)
		}
		slog.Info("gate subsystem started", "routes", len(cfg.Gate.Routes))
		close(gateReady)

		// Block until context is cancelled (signal or sibling failure)
		<-ctx.Done()
		gate.Stop()
		return nil
	})

	// ── Subsystem 2: Control (HTTP/WSS + account services) ──
	g.Go(func() error {
		ctrlCfg := toControlConfig(cfg)
		ctrl, err := ctrlEmbed.Start(ctx, ctrlCfg)
		if err != nil {
			return fmt.Errorf("control start: %w", err)
		}
		slog.Info("control subsystem started", "bind", cfg.Control.Bind)

		<-ctx.Done()
		ctrl.Stop()
		return nil
	})

	// ── Subsystem 3: Hub gRPC connection ──
	g.Go(func() error {
		if cfg.HubAddress == "" {
			slog.Warn("hub address not configured, skipping hub connection")
			<-ctx.Done()
			return nil
		}

		// Wait for gate to be ready so we can collect relay stats
		select {
		case <-gateReady:
		case <-ctx.Done():
			return nil
		}

		// Relay stats collector — converts gate embed snapshots to hubconn type
		statsFunc := func() []hubconn.RelaySnapshot {
			if gate == nil {
				return nil
			}
			gateStats := gate.RelayStats()
			out := make([]hubconn.RelaySnapshot, len(gateStats))
			for i, s := range gateStats {
				out[i] = hubconn.RelaySnapshot{
					Name: s.Name, Active: s.Active,
					Accepted: s.Accepted, Rejected: s.Rejected,
					UpstreamHealthy: s.UpstreamHealthy,
				}
			}
			return out
		}

		// Hub client pointer — set after creation, used by command handler closure
		var hubClient *hubconn.Client

		// Command handler — dispatches Hub commands to local subsystems
		cmdHandler := func(cmd *pb.HubCommand) {
			switch c := cmd.Command.(type) {
			case *pb.HubCommand_BanIp:
				if gate != nil {
					dur := time.Duration(c.BanIp.DurationMs) * time.Millisecond
					gate.Banlist().Ban(c.BanIp.Ip, c.BanIp.Reason, dur)
					slog.Info("hub command: ban", "ip", c.BanIp.Ip, "reason", c.BanIp.Reason, "duration", dur)
				}
			case *pb.HubCommand_UnbanIp:
				if gate != nil {
					gate.Banlist().Unban(c.UnbanIp.Ip)
					slog.Info("hub command: unban", "ip", c.UnbanIp.Ip)
				}
			case *pb.HubCommand_KickPlayer:
				slog.Info("hub command: kick", "account", c.KickPlayer.Account, "reason", c.KickPlayer.Reason)
				// TODO: bridge to control WSS hub for player kick
			case *pb.HubCommand_ConfigUpdate:
				slog.Info("hub command: config update", "reason", c.ConfigUpdate.Reason)
				// Trigger async config re-fetch via FetchConfig RPC
				go func() {
					fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
					defer cancel()
					newCfg, err := hubClient.FetchConfig(fetchCtx)
					if err != nil {
						slog.Error("config re-fetch failed", "err", err)
						return
					}
					slog.Info("config re-fetched",
						"tenant", newCfg.TenantId,
						"servers", len(newCfg.Servers),
						"patch_url", newCfg.PatchManifestUrl)
					// NOTE: Full dynamic route reload is not yet implemented.
					// Currently logs the new config for operator visibility.
					// Route changes require agent restart.
				}()
			case *pb.HubCommand_Announcement:
				slog.Info("hub command: announcement", "severity", c.Announcement.Severity, "message", c.Announcement.Message)
			}
		}

		hubClient = hubconn.NewClient(cfg.HubAddress, cfg.AgentToken, statsFunc, cmdHandler)

		// Keep ban count updated
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if gate != nil {
						hubClient.SetBanCount(int32(gate.Banlist().Size()))
					}
				}
			}
		}()

		slog.Info("hub connection starting", "address", cfg.HubAddress)
		return hubClient.Run(ctx)
	})

	// Wait for all subsystems to complete (triggered by signal or error)
	if err := g.Wait(); err != nil {
		slog.Error("shutdown with error", "err", err)
	} else {
		slog.Info("clean shutdown")
	}
}

// toGateConfig converts agent's flat config to gate embed's typed config.
func toGateConfig(cfg *AgentConfig) gateEmbed.GateConfig {
	routes := make([]gateEmbed.RouteConfig, len(cfg.Gate.Routes))
	for i, r := range cfg.Gate.Routes {
		dtms := r.DialTimeoutMS
		if dtms == 0 {
			dtms = cfg.Gate.Defaults.DialTimeoutMS
		}
		routes[i] = gateEmbed.RouteConfig{
			Name:          r.Name,
			Listen:        r.Listen,
			Upstream:      r.Upstream,
			ProxyProtocol: r.ProxyProtocol,
			DialTimeoutMS: dtms,
		}
	}
	return gateEmbed.GateConfig{
		Instance:    cfg.TenantSlug + "-gate",
		BanlistFile: cfg.Gate.BanlistFile,
		AdminBind:   cfg.Gate.AdminBind,
		Routes:      routes,
		Defaults: gateEmbed.DefenseDefaults{
			MaxConnPerIP: cfg.Gate.Defaults.MaxConnPerIP,
			RatePerSec:   cfg.Gate.Defaults.RatePerSec,
			Burst:        cfg.Gate.Defaults.Burst,
		},
	}
}

// toControlConfig converts agent's flat config to control embed's typed config.
func toControlConfig(cfg *AgentConfig) ctrlEmbed.ControlConfig {
	return ctrlEmbed.ControlConfig{
		Bind:      cfg.Control.Bind,
		JWTSecret: cfg.Control.JWTSecret,
		DB58:      cfg.Control.DB58,
		DB48:      cfg.Control.DB48,
		AdminUser: cfg.Control.AdminUser,
		AdminPass: cfg.Control.AdminPass,
		WebDir:    cfg.Control.WebDir,
		Launcher: tenant.LauncherWireConfig{
			PublicGateIP: "127.0.0.1", // updated by Hub config push in C-3
		},
	}
}

func loadConfig(path string) (*AgentConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg AgentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.TenantSlug == "" {
		return nil, fmt.Errorf("tenant_slug required")
	}
	if cfg.AgentToken == "" {
		return nil, fmt.Errorf("agent_token required")
	}
	if cfg.Control.Bind == "" {
		cfg.Control.Bind = "0.0.0.0:10443"
	}
	// SECURITY (P1, insecure default fallback): previously an empty JWTSecret
	// was silently replaced with "default-insecure-secret-change-me", which
	// meant a misconfigured agent would still boot with a predictable signing
	// key — trivially forgeable admin tokens. Now we refuse to start; ops
	// must supply a strong secret (>=32 chars) explicitly.
	if cfg.Control.JWTSecret == "" {
		return nil, fmt.Errorf("control.jwt_secret required (>=32 chars; generate with `openssl rand -hex 32`)")
	}
	if len(cfg.Control.JWTSecret) < 32 {
		return nil, fmt.Errorf("control.jwt_secret must be at least 32 characters (HS256 security floor)")
	}
	if cfg.Control.AdminUser == "" {
		cfg.Control.AdminUser = "admin"
	}
	// Admin password: same reasoning as JWT secret. "changeme" is worse than
	// no default because it ships an identical credential to every tenant.
	if cfg.Control.AdminPass == "" {
		return nil, fmt.Errorf("control.admin_pass required (no default — pick a strong password per tenant)")
	}
	if cfg.Gate.BanlistFile == "" {
		cfg.Gate.BanlistFile = cfg.TenantSlug + "-bans.json"
	}
	return &cfg, nil
}
