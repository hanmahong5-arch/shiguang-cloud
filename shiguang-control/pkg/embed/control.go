// Package embed provides a façade for embedding the control subsystem into
// the unified shiguang-agent binary. Wraps internal packages (handlers, hub,
// service, middleware) behind a clean Start/Stop lifecycle interface.
package embed

import (
	"context"
	"log"
	"os"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"

	"github.com/shiguang/control/internal/handlers"
	"github.com/shiguang/control/internal/hub"
	"github.com/shiguang/control/internal/middleware"
	"github.com/shiguang/control/internal/service"
	"github.com/shiguang/shared/aiondb"
	"github.com/shiguang/shared/tenant"
)

// ControlConfig is the agent-facing configuration for the control subsystem.
type ControlConfig struct {
	Bind      string // HTTP listen address
	JWTSecret string
	DB58      string // pgx DSN for 5.8 line (empty = disabled)
	DB48      string // pgx DSN for 4.8 line (empty = disabled)
	AdminUser string
	AdminPass string
	WebDir    string // admin SPA static dir (empty = disabled)

	// Launcher carries operational knobs served to launchers.
	// In agent mode this is populated from the Hub's config push.
	Launcher tenant.LauncherWireConfig
}

// ControlInstance is a running control subsystem.
type ControlInstance struct {
	app    *fiber.App
	wsHub  *hub.Hub
	pool58 *aiondb.Pool
	pool48 *aiondb.Pool
}

// Start initializes Fiber HTTP + WebSocket + account services + admin handlers.
func Start(ctx context.Context, cfg ControlConfig) (*ControlInstance, error) {
	// DB pools (optional; missing DB = feature degraded not crashed)
	var svc58, svc48 service.AccountService
	var pool58, pool48 *aiondb.Pool
	var err error

	if cfg.DB58 != "" {
		pool58, err = aiondb.Open(ctx, cfg.DB58)
		if err != nil {
			log.Printf("[control] 5.8 DB unreachable: %v", err)
		} else {
			svc58 = service.NewAccountService58(pool58)
			log.Printf("[control] 5.8 DB connected")
		}
	}
	if cfg.DB48 != "" {
		pool48, err = aiondb.Open(ctx, cfg.DB48)
		if err != nil {
			log.Printf("[control] 4.8 DB unreachable: %v", err)
		} else {
			svc48 = service.NewAccountService48(pool48)
			log.Printf("[control] 4.8 DB connected")
		}
	}

	// WSS hub
	h := hub.NewHub()
	go h.Run()

	// Fiber app
	app := fiber.New(fiber.Config{
		AppName:               "shiguang-control",
		DisableStartupMessage: true,
		ReadTimeout:           10e9,
	})
	app.Use(fiberrecover.New())
	app.Use(fiberlogger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	// Handlers
	api := app.Group("/api")

	accountH := handlers.NewAccountHandler(svc58, svc48)
	accountH.Register(api)

	launcherH := handlers.NewLauncherHandler(&cfg.Launcher)
	launcherH.Register(api)

	jwtCfg := middleware.JWTConfig{
		Secret:  cfg.JWTSecret,
		Issuer:  "shiguang-control",
		TTLDays: 7,
	}
	adminH := handlers.NewAdminHandler(jwtCfg, nil, h, cfg.AdminUser, cfg.AdminPass)
	adminH.Register(api)

	// WebSocket
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
		client := hub.NewClient(uuid.New().String(), account, server, conn)
		h.Attach(client)
	}))

	// Health
	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	// Admin SPA static hosting
	if cfg.WebDir != "" {
		if _, err := os.Stat(cfg.WebDir); err == nil {
			app.Static("/admin", cfg.WebDir, fiber.Static{Index: "index.html"})
			log.Printf("[control] admin SPA from %s at /admin", cfg.WebDir)
		}
	}

	// Start listener in background goroutine
	go func() {
		log.Printf("[control] listening on %s", cfg.Bind)
		if err := app.Listen(cfg.Bind); err != nil {
			log.Printf("[control] listen error: %v", err)
		}
	}()

	return &ControlInstance{
		app:    app,
		wsHub:  h,
		pool58: pool58,
		pool48: pool48,
	}, nil
}

// Stop gracefully shuts down Fiber and closes DB pools.
func (c *ControlInstance) Stop() {
	if c.app != nil {
		_ = c.app.Shutdown()
	}
	if c.pool58 != nil {
		c.pool58.Close()
	}
	if c.pool48 != nil {
		c.pool48.Close()
	}
	log.Printf("[control] stopped")
}

// Hub returns the WSS hub for external command injection.
func (c *ControlInstance) Hub() *hub.Hub { return c.wsHub }

