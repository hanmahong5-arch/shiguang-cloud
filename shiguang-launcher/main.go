package main

import (
	"embed"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "拾光登录器",
		Width:  1024,
		Height: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// overlayHandler serves brand-specific assets (logo, bg) from
			// the local disk cache (~/.shiguang-launcher/brands/{code}/).
			// If a requested file exists on disk, it's served from there;
			// otherwise Wails falls through to the embedded assets.
			// This enables runtime white-labeling without recompilation.
			Handler: newOverlayHandler(),
		},
		BackgroundColour: &options.RGBA{R: 15, G: 20, B: 30, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// overlayHandler serves brand assets from the disk cache directory,
// falling back to Wails' embedded assets (embed.FS) for everything else.
// This enables each tenant's logo/background to override the defaults
// without recompiling the launcher.
type overlayHandler struct {
	brandBaseDir string // ~/.shiguang-launcher/brands/
}

func newOverlayHandler() *overlayHandler {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return &overlayHandler{
		brandBaseDir: filepath.Join(home, ".shiguang-launcher", "brands"),
	}
}

// ServeHTTP checks if the requested path exists in the brand assets directory.
// Requests like /brand-assets/logo.png → ~/.shiguang-launcher/brands/{current}/logo.png
// If found, serve from disk. If not, return 404 (Wails will serve from embed.FS).
func (h *overlayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only handle paths under /brand-assets/
	if len(r.URL.Path) < 15 || r.URL.Path[:14] != "/brand-assets/" {
		// Not a brand asset request — let Wails serve from embed.FS
		http.NotFound(w, r)
		return
	}

	// Strip the /brand-assets/ prefix and serve from disk
	relPath := filepath.Clean(r.URL.Path[14:])

	// Search all brand directories for the file (simple: walk the brands dir)
	// In practice, the frontend constructs URLs with known filenames
	entries, err := os.ReadDir(h.brandBaseDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		localPath := filepath.Join(h.brandBaseDir, entry.Name(), relPath)
		if _, err := os.Stat(localPath); err == nil {
			http.ServeFile(w, r, localPath)
			return
		}
	}
	http.NotFound(w, r)
}
