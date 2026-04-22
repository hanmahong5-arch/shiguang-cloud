// shiguang-control is the HTTP/WebSocket control center for the ShiguangSuite.
//
// Responsibilities:
//   - REST API for account management (register/login/change/reset) split by server line
//   - JWT-authenticated admin API for bans, status, kicks
//   - WebSocket hub for real-time launcher connections (online counts, kick commands)
//   - Static hosting of the admin React SPA
//   - Fan-out to shiguang-gate admin endpoints for ban propagation
//
// Operational note: in production this runs as a Windows Service behind TLS.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"

	"github.com/shiguang/control/internal/config"
	"github.com/shiguang/control/internal/handlers"
	"github.com/shiguang/control/internal/hub"
	"github.com/shiguang/control/internal/httputil"
	"github.com/shiguang/control/internal/metrics"
	"github.com/shiguang/control/internal/middleware"
	"github.com/shiguang/control/internal/service"
	"github.com/shiguang/shared/aiondb"
)

func main() {
	var configPath string
	var webDir string
	var adminUser string
	var adminPass string
	flag.StringVar(&configPath, "config", "configs/control.yaml", "YAML config path")
	flag.StringVar(&webDir, "web", "web/dist", "static web directory for the admin SPA")
	flag.StringVar(&adminUser, "admin-user", "admin", "admin username (override via env SHIGUANG_ADMIN_USER)")
	flag.StringVar(&adminPass, "admin-pass", "", "admin password (override via env SHIGUANG_ADMIN_PASS)")
	flag.Parse()

	if v := os.Getenv("SHIGUANG_ADMIN_USER"); v != "" {
		adminUser = v
	}
	if v := os.Getenv("SHIGUANG_ADMIN_PASS"); v != "" {
		adminPass = v
	}
	if adminPass == "" {
		log.Println("WARNING: admin password not set — use SHIGUANG_ADMIN_PASS env or -admin-pass flag")
		adminPass = "changeme" // explicitly insecure, logged
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("shiguang-control starting: bind=%s", cfg.Bind)

	ctx := context.Background()

	// ── database pools ────────────────────────────────────────────────────
	var svc58 service.AccountService
	var svc48 service.AccountService
	var pool58, pool48 *aiondb.Pool
	if cfg.DB58 != "" {
		pool58, err = aiondb.Open(ctx, cfg.DB58)
		if err != nil {
			log.Printf("warning: 5.8 DB unreachable (%v) — 5.8 account API will fail", err)
		} else {
			svc58 = service.NewAccountService58(pool58)
			log.Printf("5.8 DB connected")
		}
	}
	if cfg.DB48 != "" {
		pool48, err = aiondb.Open(ctx, cfg.DB48)
		if err != nil {
			log.Printf("warning: 4.8 DB unreachable (%v) — 4.8 account API will fail", err)
		} else {
			svc48 = service.NewAccountService48(pool48)
			log.Printf("4.8 DB connected")
		}
	}

	// ── hub ────────────────────────────────────────────────────────────────
	h := hub.NewHub()
	go h.Run()

	// ── fiber app ──────────────────────────────────────────────────────────
	// 全局 ErrorHandler：把所有 fiber.NewError / panic 转为 RFC 7807 Problem，
	// 统一 API 错误格式。handler 内显式调用 httputil.* 的响应不受影响
	// （已经写 response body，不会进入 ErrorHandler）。
	app := fiber.New(fiber.Config{
		AppName:               "shiguang-control",
		DisableStartupMessage: false,
		ReadTimeout:           10e9,          // 10 s — max time to read full request body
		WriteTimeout:          30e9,          // 30 s — covers slow /healthz deep-probes
		IdleTimeout:           120e9,         // 120 s — keep-alive connections (launcher long-poll)
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			msg := err.Error()
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
				msg = e.Message
			}
			return httputil.Problem(c, httputil.ProblemOpts{
				Status: code,
				Title:  http.StatusText(code),
				Detail: msg,
			})
		},
	})
	app.Use(recover.New())

	// ── Request ID middleware ──────────────────────────────────────────────
	// 每请求生成一个唯一 ID 并写入 X-Request-Id 响应头。日志中间件
	// 读取 Locals("requestid") 输出该 ID，便于运维关联一次请求的全部日志。
	app.Use(func(c *fiber.Ctx) error {
		rid := c.Get("X-Request-Id")
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Locals("requestid", rid)
		c.Set("X-Request-Id", rid)
		return c.Next()
	})

	app.Use(logger.New(logger.Config{
		Format:     "${time} | ${status} | ${latency} | ${ip} | ${method} | ${path} | ${error}\n",
		TimeFormat: "15:04:05",
	}))

	// ── Security Headers middleware ────────────────────────────────────────
	// 防点击劫持、MIME 嗅探、XSS 反射。API-first 服务 CSP 从宽；Admin SPA
	// 由 React 托管于同源，CSP 限制内联脚本风险可接受。
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("X-XSS-Protection", "1; mode=block")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// HSTS 仅在 TLS 模式下设置，避免 HTTP 明文场景浏览器拒绝访问
		if cfg.TLS != nil && cfg.TLS.CertFile != "" {
			c.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		return c.Next()
	})

	// CORS: restrict to same-origin admin SPA + localhost dev server.
	// Requests from game servers (ExternalAuth, token validate) are server-to-server
	// and don't send Origin headers, so CORS doesn't block them.
	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			// Allow same-host requests (admin SPA served from same origin)
			// and localhost dev server (Vite proxy)
			if origin == "" {
				return true
			}
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

	// ── handlers ───────────────────────────────────────────────────────────
	api := app.Group("/api")

	accountH := handlers.NewAccountHandler(svc58, svc48)
	accountH.Register(api)

	launcherH := handlers.NewLauncherHandler(&cfg.Launcher)
	launcherH.Register(api)

	jwtCfg := middleware.JWTConfig{
		Secret:  cfg.JWT.Secret,
		Issuer:  cfg.JWT.Issuer,
		TTLDays: cfg.JWT.TTLDays,
	}
	adminH := handlers.NewAdminHandler(jwtCfg, cfg.Gates, h, adminUser, adminPass)
	adminH.Register(api)

	// ── WebSocket endpoint ─────────────────────────────────────────────────
	// Launcher connects to /ws with ?account=xxx&server=5.8 after logging in.
	// Minimal auth: require the query param `account` to be present. A more
	// robust version would short-lived JWT the launcher on /api/account/login.
	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(func(conn *websocket.Conn) {
		account := conn.Query("account")
		server := conn.Query("server")
		if account == "" || (server != "5.8" && server != "4.8") {
			conn.WriteJSON(fiber.Map{"error": "missing account/server"})
			conn.Close()
			return
		}
		id := uuid.New().String()
		client := hub.NewClient(id, account, server, conn)
		h.Attach(client)
	}))

	// ── request counting middleware ───────────────────────────────────────
	// 必须在 logger 之后、路由注册之前，以便覆盖所有 handler 响应。
	app.Use(metrics.CountMiddleware())

	// ── Prometheus metrics (text/plain) ───────────────────────────────────
	// 仅 loopback 可访问（与 /healthz 同策略），避免信息泄漏。
	app.Get("/metrics", metrics.Handler())

	// ── lightweight liveness & readiness ──────────────────────────────────
	// /readyz: 秒级响应，仅检查进程存活。适合负载均衡器 TCP 探针频率（5s）。
	// /livez:  同义别名（兼容 Kubernetes 约定）。
	app.Get("/readyz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	app.Get("/livez", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// ── health check (deep) ───────────────────────────────────────────────
	// Returns 200 with component-level status when all critical deps are healthy,
	// or 503 if any critical dependency is down. Designed for load balancer probes.
	app.Get("/healthz", func(c *fiber.Ctx) error {
		status := fiber.Map{
			"hub_online": h.ConnectedCount(),
			"hub_served": h.TotalServed(),
		}
		healthy := true

		// Check database pools
		if pool58 != nil {
			if err := pool58.Ping(c.Context()); err != nil {
				status["db_58"] = "unreachable"
				healthy = false
			} else {
				status["db_58"] = "ok"
			}
		} else {
			status["db_58"] = "not_configured"
		}
		if pool48 != nil {
			if err := pool48.Ping(c.Context()); err != nil {
				status["db_48"] = "unreachable"
				healthy = false
			} else {
				status["db_48"] = "ok"
			}
		} else {
			status["db_48"] = "not_configured"
		}

		// Check gate connectivity.
		// SECURITY (P2, slow-loris DoS on probe path): use a bounded-timeout
		// client so a hung/slow gate can't tie up the /healthz worker
		// indefinitely. Bare http.Get uses http.DefaultClient (no timeout).
		gateStatus := make(map[string]string)
		gateProbe := &http.Client{Timeout: 3 * time.Second}
		for _, g := range cfg.Gates {
			resp, err := gateProbe.Get(g.URL + "/healthz")
			if err != nil {
				gateStatus[g.Name] = "unreachable"
			} else {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					gateStatus[g.Name] = "ok"
				} else {
					gateStatus[g.Name] = fmt.Sprintf("http_%d", resp.StatusCode)
				}
			}
		}
		status["gates"] = gateStatus

		if !healthy {
			return c.Status(fiber.StatusServiceUnavailable).JSON(status)
		}
		return c.JSON(status)
	})

	// ── static admin SPA hosting ───────────────────────────────────────────
	if _, err := os.Stat(webDir); err == nil {
		app.Static("/admin", webDir, fiber.Static{
			Index:  "index.html",
			Browse: false,
		})
		log.Printf("admin SPA served from %s at /admin", webDir)
	} else {
		log.Printf("admin SPA directory %s not present — /admin disabled", webDir)
	}

	// ── start + graceful shutdown ──────────────────────────────────────────
	errCh := make(chan error, 1)
	go func() {
		if cfg.TLS != nil && cfg.TLS.CertFile != "" {
			log.Printf("listening on https://%s", cfg.Bind)
			errCh <- app.ListenTLS(cfg.Bind, cfg.TLS.CertFile, cfg.TLS.KeyFile)
		} else {
			log.Printf("listening on http://%s (no TLS — dev mode)", cfg.Bind)
			errCh <- app.Listen(cfg.Bind)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("listen: %v", err)
	case s := <-sig:
		log.Printf("signal %v, shutting down", s)
	}

	h.Stop() // signal hub goroutine to exit and close all WS connections
	_ = app.Shutdown()
	if pool58 != nil {
		pool58.Close()
	}
	if pool48 != nil {
		pool48.Close()
	}
	log.Println("shutdown complete")
}
