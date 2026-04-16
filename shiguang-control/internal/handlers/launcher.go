package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/shiguang/shared/tenant"
)

// LauncherHandler serves /api/launcher/config — the hot-editable payload
// that every launcher fetches right after authentication.
type LauncherHandler struct {
	cfg *tenant.LauncherWireConfig
}

// NewLauncherHandler wires the handler against a live config pointer.
// The caller (main) may atomically swap *cfg at runtime for hot reload.
func NewLauncherHandler(cfg *tenant.LauncherWireConfig) *LauncherHandler {
	return &LauncherHandler{cfg: cfg}
}

func (h *LauncherHandler) Register(r fiber.Router) {
	g := r.Group("/launcher")
	g.Get("/config", h.getConfig)
}

func (h *LauncherHandler) getConfig(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"public_gate_ip":     h.cfg.PublicGateIP,
		"patch_manifest_url": h.cfg.PatchManifestURL,
		"news_url":           h.cfg.NewsURL,
		"servers":            h.cfg.Servers,
	})
}
