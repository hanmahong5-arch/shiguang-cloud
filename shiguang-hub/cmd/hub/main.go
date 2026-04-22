// shiguang-hub is the cloud-hosted multi-tenant control center for ShiguangCloud.
//
// Responsibilities:
//   - Operator onboarding (registration, login, JWT)
//   - Public launcher bootstrap (resolve tenant code → brand + config)
//   - Protected operator dashboard API (server lines, branding, agents, stats)
//   - gRPC AgentHub server for gate agent heartbeat + command dispatch (Phase C-3)
//   - Static hosting of the operator dashboard SPA
//
// Game traffic NEVER flows through Hub — only control plane traffic.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"

	"github.com/shiguang/hub/handlers"
	"github.com/shiguang/hub/hubconfig"
	"github.com/shiguang/hub/internal/grpcserver"
	"github.com/shiguang/shared/aiondb"
	"github.com/shiguang/shared/sglog"
	"github.com/shiguang/shared/tenant"
)

func main() {
	var configPath string
	var dashboardDir string
	var dev bool
	flag.StringVar(&configPath, "config", "hub.yaml", "Hub config path")
	flag.StringVar(&dashboardDir, "dashboard", "admin-spa/dist", "operator dashboard SPA directory")
	flag.BoolVar(&dev, "dev", false, "development mode (human-readable logs)")
	flag.Parse()

	sglog.Init("hub", dev)

	cfg, err := hubconfig.Load(configPath)
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	slog.Info("starting", "bind", cfg.Bind, "dsn", cfg.DSN[:min(40, len(cfg.DSN))])

	ctx := context.Background()

	// ── Database ────────────────────────────────────────────────────────
	pool, err := aiondb.Open(ctx, cfg.DSN)
	if err != nil {
		slog.Error("database connection failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected")

	// Auto-create tables if they don't exist (idempotent)
	if err := tenant.AutoMigrate(ctx, pool.Pool); err != nil {
		slog.Error("schema migration failed", "err", err)
		os.Exit(1)
	}

	repo := tenant.NewRepo(pool.Pool)

	// ── Fiber app ───────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		AppName:               "shiguang-hub",
		DisableStartupMessage: true,
		ReadTimeout:           10e9,  // 10 s
		WriteTimeout:          30e9,  // 30 s — 覆盖慢响应（patch 上传等）
		IdleTimeout:           120e9, // 120 s — 保持 keep-alive
	})
	app.Use(fiberrecover.New())

	// Request ID：每请求 UUID 写入 X-Request-Id 并存入 Locals 供 logger 读取
	app.Use(func(c *fiber.Ctx) error {
		rid := c.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Locals("requestid", rid)
		c.Set("X-Request-Id", rid)
		return c.Next()
	})

	app.Use(fiberlogger.New())

	// Security Headers：与 shiguang-control 对齐，防点击劫持 / MIME 嗅探 / XSS
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if cfg.TLS != nil && cfg.TLS.CertFile != "" {
			c.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		return c.Next()
	})

	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			// Allow requests with no origin (server-to-server, curl, etc.)
			if origin == "" {
				return true
			}
			// Allow same-host admin SPA + localhost dev servers
			for _, prefix := range []string{
				"http://localhost", "http://127.0.0.1",
				"https://localhost", "https://127.0.0.1",
			} {
				if len(origin) >= len(prefix) && origin[:len(prefix)] == prefix {
					return true
				}
			}
			return false
		},
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	// ── Handlers ────────────────────────────────────────────────────────
	tenantH := handlers.NewTenantHandler(repo, cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.TTLDays)

	// Public endpoints (no auth)
	api := app.Group("/api/v1")
	tenantH.RegisterPublic(api)

	// Protected operator endpoints (JWT required)
	protected := api.Group("/operator", handlers.RequireTenant(cfg.JWT.Secret, cfg.JWT.Issuer))
	tenantH.RegisterProtected(protected)

	// Operator dashboard SPA
	if _, err := os.Stat(dashboardDir); err == nil {
		app.Static("/admin", dashboardDir, fiber.Static{Index: "index.html"})
		slog.Info("dashboard SPA enabled", "dir", dashboardDir, "path", "/admin")
	}

	// Patch file hosting — serves chunk-manifest.json and chunks/{hash}
	// per tenant code. Launcher fetches: /patches/{CODE}/chunk-manifest.json
	// and /patches/{CODE}/chunks/{sha256_hash}
	if cfg.PatchDir != "" {
		if _, err := os.Stat(cfg.PatchDir); err == nil {
			app.Static("/patches", cfg.PatchDir, fiber.Static{
				Browse:   false,
				MaxAge:   3600, // 1 hour cache — chunks are immutable (content-addressed)
				Compress: true, // gzip for manifest JSON
			})
			slog.Info("patch hosting enabled", "dir", cfg.PatchDir, "path", "/patches")
		} else {
			slog.Warn("patch_dir not found, skipping", "dir", cfg.PatchDir)
		}
	}

	// ── gRPC AgentHub server ────────────────────────────────────────────
	grpcSrv := grpcserver.NewServer(repo)
	grpcBind := cfg.GRPCBind
	if grpcBind == "" {
		grpcBind = "0.0.0.0:50051"
	}
	grpcGo, err := grpcSrv.Start(grpcBind)
	if err != nil {
		slog.Error("gRPC server failed", "err", err)
		os.Exit(1)
	}
	slog.Info("gRPC AgentHub started", "bind", grpcBind)

	// Lightweight probes（与 control 对齐，秒级响应，适合 LB TCP 探针）
	app.Get("/readyz", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/livez", func(c *fiber.Ctx) error { return c.SendString("ok") })

	// Health check — pings DB, reports gRPC agent count, returns 503 if critical deps are down
	app.Get("/healthz", func(c *fiber.Ctx) error {
		httpStatus := fiber.StatusOK
		dbOK := true

		pingCtx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Pool.Ping(pingCtx); err != nil {
			dbOK = false
			httpStatus = fiber.StatusServiceUnavailable
		}

		return c.Status(httpStatus).JSON(fiber.Map{
			"status":      map[bool]string{true: "healthy", false: "degraded"}[httpStatus == fiber.StatusOK],
			"database":    map[bool]string{true: "ok", false: "unreachable"}[dbOK],
			"grpc_agents": grpcSrv.ConnectedAgents(),
		})
	})

	// ── Background: expired token cleanup ───────────────────────────────
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			purgeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			n, err := repo.PurgeExpiredTokens(purgeCtx)
			cancel()
			if err != nil {
				slog.Error("token purge failed", "err", err)
			} else if n > 0 {
				slog.Info("expired tokens purged", "count", n)
			}
		}
	}()

	// ── Start ───────────────────────────────────────────────────────────
	go func() {
		slog.Info("HTTP server starting", "bind", cfg.Bind, "tls", cfg.TLS != nil && cfg.TLS.CertFile != "")
		if cfg.TLS != nil && cfg.TLS.CertFile != "" {
			if err := app.ListenTLS(cfg.Bind, cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil {
				slog.Error("listen failed", "err", err)
				os.Exit(1)
			}
		} else {
			if err := app.Listen(cfg.Bind); err != nil {
				slog.Error("listen failed", "err", err)
				os.Exit(1)
			}
		}
	}()

	// ── Graceful shutdown ───────────────────────────────────────────────
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	s := <-sig
	slog.Info("shutdown signal received", "signal", s.String())
	grpcGo.GracefulStop()
	_ = app.Shutdown()
	slog.Info("shutdown complete", "agents_connected", grpcSrv.ConnectedAgents())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
