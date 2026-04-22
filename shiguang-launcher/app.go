package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	runtimePkg "runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/shiguang/launcher/internal/control"
	"github.com/shiguang/launcher/internal/game"
	"github.com/shiguang/launcher/internal/patching"
)

// Version 语义化版本号，CI 可通过 -ldflags "-X main.Version=..." 覆盖。
// 前端通过 GetVersion() 查询。
var Version = "0.2.0-dev"

// BuildTime 可在构建时由 CI 注入（-ldflags "-X main.BuildTime=2026-04-15T12:34:56Z"）。
var BuildTime = ""

// VersionInfo 供前端展示 About 卡片使用。
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// App is the Wails bound struct. All exported methods become JS functions
// on the frontend via `window.go.main.App.*`.
//
// Concurrency: Wails serializes calls per-method, but we may run long-lived
// operations (patching, WSS) in background goroutines that emit events via
// runtime.EventsEmit so the React UI stays responsive.
type App struct {
	ctx context.Context

	mu           sync.Mutex
	controlURL   string          // from prefs or env
	client       *control.Client // lazy-initialised on first use
	prefs        *Prefs          // loaded from disk on startup
	brand        *BrandConfig    // cached white-label branding (nil = no server code set)
	sessionToken string          // last login session token for Token Handoff
	cachedConfig *control.LauncherConfig // 最近一次成功的 launcher config 离线缓存

	patchCancel context.CancelFunc // active patch job, if any
}

// Prefs is persisted to ~/.shiguang-launcher/prefs.json.
type Prefs struct {
	ControlURL  string            `json:"control_url"`  // e.g. "https://control:10443"
	ClientPaths map[string]string `json:"client_paths"` // {"5.8": "D:\\aion\\client-5.8", "4.8": "..."}
	ServerCode  string            `json:"server_code"`  // tenant invite code (e.g. "JUEZHAN")
}

// BrandConfig is the white-label branding fetched from Hub for the current
// tenant. Cached locally so the launcher can start with branding even offline.
type BrandConfig struct {
	ServerCode  string `json:"server_code"`
	ServerName  string `json:"server_name"`   // title bar text
	LogoURL     string `json:"logo_url"`
	BgURL       string `json:"bg_url"`
	AccentColor string `json:"accent_color"`  // e.g. "#ff4500"
	TextColor   string `json:"text_color"`    // e.g. "#e6e8eb"
	ControlURL  string `json:"control_url"`   // agent's control API URL
	GateIP      string `json:"gate_ip"`
	NewsURL     string `json:"news_url"`
	Servers     []BrandServer `json:"servers"`
}

// BrandServer is a simplified server line for the launcher UI.
type BrandServer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	AuthPort int    `json:"auth_port"`
	GameArgs string `json:"game_args"`
}

// NewApp creates a new App.
func NewApp() *App {
	return &App{}
}

// startup is called by Wails on app launch.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.prefs = loadPrefs()

	// Load cached brand (if server code was previously set)
	a.brand = loadBrandCache(a.prefs.ServerCode)

	// Determine control URL: brand > prefs > env > default
	a.controlURL = ""
	if a.brand != nil && a.brand.ControlURL != "" {
		a.controlURL = a.brand.ControlURL
	}
	if a.controlURL == "" {
		a.controlURL = envOr("SHIGUANG_CONTROL_URL", a.prefs.ControlURL)
	}
	if a.controlURL == "" {
		a.controlURL = "http://127.0.0.1:10443"
	}
	a.client = control.NewClient(a.controlURL)
	a.client.SetEventHandler(a.onWSEvent)

	// Emit cached brand immediately so React renders fast
	if a.brand != nil {
		runtime.EventsEmit(ctx, "brand:loaded", a.brand)
	}

	// Async: try to refresh brand from Hub (non-blocking)
	if a.prefs.ServerCode != "" {
		go a.refreshBrand(a.prefs.ServerCode)
	}
}

// onWSEvent bridges WSS envelopes from the control center into Wails events
// for the React UI to react to.
func (a *App) onWSEvent(envelopeType string, payload json.RawMessage) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "control:"+envelopeType, string(payload))
}

// ---- frontend-bound methods ----

// GetControlURL returns the current control center URL (for Settings display).
func (a *App) GetControlURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.controlURL
}

// SetControlURL updates the control center URL and re-creates the client.
func (a *App) SetControlURL(u string) error {
	if u == "" {
		return fmt.Errorf("url required")
	}
	a.mu.Lock()
	a.controlURL = u
	a.prefs.ControlURL = u
	a.client = control.NewClient(u)
	a.client.SetEventHandler(a.onWSEvent)
	a.mu.Unlock()
	return savePrefs(a.prefs)
}

// SetClientPath saves the local path to the game client for a given server line.
func (a *App) SetClientPath(server, path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.prefs.ClientPaths == nil {
		a.prefs.ClientPaths = make(map[string]string)
	}
	a.prefs.ClientPaths[server] = path
	return savePrefs(a.prefs)
}

// GetPrefs returns the persisted preferences (for Settings screen hydration).
func (a *App) GetPrefs() *Prefs {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prefs
}

// GetVersion 返回启动器自身的版本信息，供前端 About 卡片展示。
// Version / BuildTime 在构建时由 ldflags 注入，未注入则使用包级默认值。
func (a *App) GetVersion() VersionInfo {
	return VersionInfo{
		Version:   Version,
		BuildTime: BuildTime,
		GoVersion: runtimePkg.Version(),
		Platform:  runtimePkg.GOOS + "/" + runtimePkg.GOARCH,
	}
}

// FetchLauncherConfig pulls the hot-edited launcher config from control.
//
// Retrieval strategy:
//   - 最多 3 次尝试（初次 + 2 次退避重试），间隔 500ms → 1.5s
//   - 全部失败时若有缓存则返回缓存并 emit warning 事件（offline fallback）
//   - 成功时更新内存缓存（cachedConfig 原子覆写，无锁竞争）
//   - 超时仍为 10s（含重试总预算），每次尝试使用子 context 切分
func (a *App) FetchLauncherConfig() (*control.LauncherConfig, error) {
	const maxAttempts = 3
	backoffs := [...]time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	var lastErr error
retry:
	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				// 父 context 超时：立即退出重试循环，break retry 才能跳出 for
				// （普通 break 只跳出 select，会继续下一轮重试并覆盖 lastErr）
				lastErr = ctx.Err()
				break retry
			case <-time.After(backoffs[i]):
			}
		}
		cfg, err := a.client.FetchLauncherConfig(ctx)
		if err == nil {
			// 成功：更新缓存
			a.cachedConfig = cfg
			return cfg, nil
		}
		lastErr = err
		log.Printf("[launcher] FetchLauncherConfig attempt %d/%d failed: %v", i+1, maxAttempts, err)
	}

	// 全部失败：离线 fallback
	if a.cachedConfig != nil {
		log.Printf("[launcher] using cached config (control unreachable)")
		runtime.EventsEmit(a.ctx, "control:offline", map[string]any{
			"error": lastErr.Error(),
		})
		return a.cachedConfig, nil
	}
	return nil, fmt.Errorf("control unreachable after %d attempts: %w", maxAttempts, lastErr)
}

// Register, Login, ChangePassword, ResetPassword — thin wrappers around control.

func (a *App) Register(server, name, password, email string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return a.client.Register(ctx, server, name, password, email)
}

func (a *App) Login(server, name, password string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	token, err := a.client.Login(ctx, server, name, password)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.sessionToken = token
	a.mu.Unlock()
	// Open WebSocket after successful login
	return a.client.ConnectWS()
}

func (a *App) Logout() {
	a.client.DisconnectWS()
}

func (a *App) ChangePassword(server, name, oldPw, newPw string) error {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return a.client.ChangePassword(ctx, server, name, oldPw, newPw)
}

func (a *App) ResetPassword(server, name, email string) (string, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return a.client.ResetPassword(ctx, server, name, email)
}

// StartPatch kicks off the patcher in the background. Emits:
//   - "patch:progress"  { phase, done, total, file }
//   - "patch:complete"  {}
//   - "patch:error"     { error }
func (a *App) StartPatch(server string) error {
	a.mu.Lock()
	clientRoot := a.prefs.ClientPaths[server]
	a.mu.Unlock()
	if clientRoot == "" {
		return fmt.Errorf("client path not set for server %q", server)
	}

	// Fetch latest manifest URL from control
	cfg, err := a.FetchLauncherConfig()
	if err != nil {
		return fmt.Errorf("fetch config: %w", err)
	}

	ctx, cancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	if a.patchCancel != nil {
		a.patchCancel() // cancel any previous patch
	}
	a.patchCancel = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.patchCancel = nil
			a.mu.Unlock()
		}()
		p := patching.NewPatcher(clientRoot, cfg.PatchManifestURL,
			func(phase string, done, total int64, file string) {
				runtime.EventsEmit(a.ctx, "patch:progress", map[string]any{
					"phase": phase, "done": done, "total": total, "file": file,
				})
			})
		if err := p.Run(ctx); err != nil {
			runtime.EventsEmit(a.ctx, "patch:error", map[string]string{"error": err.Error()})
			return
		}
		runtime.EventsEmit(a.ctx, "patch:complete", map[string]bool{"ok": true})
	}()
	return nil
}

// CancelPatch aborts the running patch job (if any).
func (a *App) CancelPatch() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.patchCancel != nil {
		a.patchCancel()
		a.patchCancel = nil
	}
}

// LaunchGame starts the AION client for the given server line.
// Requires a prior successful Login and a valid client path.
func (a *App) LaunchGame(server string) (int, error) {
	// Fetch latest config to get public_gate_ip + per-line auth_port + game_args
	cfg, err := a.FetchLauncherConfig()
	if err != nil {
		return 0, fmt.Errorf("fetch config: %w", err)
	}

	var line *control.ServerLine
	for i := range cfg.Servers {
		if cfg.Servers[i].ID == server {
			line = &cfg.Servers[i]
			break
		}
	}
	if line == nil {
		return 0, fmt.Errorf("server %q not in control config", server)
	}

	a.mu.Lock()
	clientRoot := a.prefs.ClientPaths[server]
	a.mu.Unlock()
	if clientRoot == "" {
		return 0, fmt.Errorf("client path not set for server %q", server)
	}

	a.mu.Lock()
	token := a.sessionToken
	a.mu.Unlock()

	return game.Start(game.StartConfig{
		ClientRoot:   clientRoot,
		ServerID:     server,
		GateIP:       cfg.PublicGateIP,
		AuthPort:     line.AuthPort,
		ExtraArgs:    line.GameArgs,
		SessionToken: token,
	})
}

// ---- brand / white-label ----

// GetBrand returns the current brand config (nil if no server code set).
// Called by React on mount to determine if ActivationScreen should show.
func (a *App) GetBrand() *BrandConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.brand
}

// SetServerCode is called from the ActivationScreen when the player enters
// a tenant invite code for the first time. Downloads brand from Hub, caches
// locally, and emits "brand:loaded" event for the React UI.
func (a *App) SetServerCode(code string) (*BrandConfig, error) {
	if code == "" {
		return nil, fmt.Errorf("server code required")
	}

	// Fetch brand from Hub
	hubURL := envOr("SHIGUANG_HUB_URL", "https://hub.shiguang.cloud")
	url := hubURL + "/api/v1/public/" + code + "/bootstrap"

	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()

	brand, err := fetchBrandFromHub(ctx, url, code)
	if err != nil {
		return nil, fmt.Errorf("fetch brand: %w", err)
	}

	// Cache to disk
	saveBrandCache(code, brand)

	// Update state
	a.mu.Lock()
	a.brand = brand
	a.prefs.ServerCode = code
	if brand.ControlURL != "" {
		a.controlURL = brand.ControlURL
		a.client = control.NewClient(brand.ControlURL)
		a.client.SetEventHandler(a.onWSEvent)
	}
	a.mu.Unlock()

	_ = savePrefs(a.prefs)

	// Notify React
	runtime.EventsEmit(a.ctx, "brand:loaded", brand)

	return brand, nil
}

// ClearServerCode resets the brand and shows the activation screen again.
func (a *App) ClearServerCode() {
	a.mu.Lock()
	a.brand = nil
	a.prefs.ServerCode = ""
	a.mu.Unlock()
	_ = savePrefs(a.prefs)
	runtime.EventsEmit(a.ctx, "brand:cleared", true)
}

// refreshBrand silently refreshes the brand from Hub in the background.
func (a *App) refreshBrand(code string) {
	hubURL := envOr("SHIGUANG_HUB_URL", "https://hub.shiguang.cloud")
	url := hubURL + "/api/v1/public/" + code + "/bootstrap"

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	brand, err := fetchBrandFromHub(ctx, url, code)
	if err != nil {
		return // silent failure — use cached brand
	}

	a.mu.Lock()
	a.brand = brand
	if brand.ControlURL != "" && brand.ControlURL != a.controlURL {
		a.controlURL = brand.ControlURL
		a.client = control.NewClient(brand.ControlURL)
		a.client.SetEventHandler(a.onWSEvent)
	}
	a.mu.Unlock()

	saveBrandCache(code, brand)
	runtime.EventsEmit(a.ctx, "brand:loaded", brand)
}

// fetchBrandFromHub calls the Hub bootstrap API and converts to BrandConfig.
func fetchBrandFromHub(ctx context.Context, url, code string) (*BrandConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// The Hub bootstrap response is a superset; extract the brand-relevant fields.
	var raw struct {
		TenantName string `json:"tenant_name"`
		GateIP     string `json:"gate_ip"`
		Theme      struct {
			ServerName  string `json:"server_name"`
			LogoURL     string `json:"logo_url"`
			BgURL       string `json:"bg_url"`
			AccentColor string `json:"accent_color"`
			TextColor   string `json:"text_color"`
			NewsURL     string `json:"news_url"`
			PatchURL    string `json:"patch_url"`
		} `json:"theme"`
		ServerLines []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Version  string `json:"version"`
			AuthPort int    `json:"auth_port"`
			GameArgs string `json:"game_args"`
		} `json:"server_lines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	brand := &BrandConfig{
		ServerCode:  code,
		ServerName:  raw.Theme.ServerName,
		LogoURL:     raw.Theme.LogoURL,
		BgURL:       raw.Theme.BgURL,
		AccentColor: raw.Theme.AccentColor,
		TextColor:   raw.Theme.TextColor,
		GateIP:      raw.GateIP,
		NewsURL:     raw.Theme.NewsURL,
	}
	if brand.ServerName == "" {
		brand.ServerName = raw.TenantName
	}
	for _, s := range raw.ServerLines {
		brand.Servers = append(brand.Servers, BrandServer{
			ID: s.Version, Name: s.Name, AuthPort: s.AuthPort, GameArgs: s.GameArgs,
		})
	}
	return brand, nil
}

// ---- brand cache persistence ----

func brandCacheDir(code string) string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".shiguang-launcher", "brands", code)
}

func loadBrandCache(code string) *BrandConfig {
	if code == "" {
		return nil
	}
	path := filepath.Join(brandCacheDir(code), "brand.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var b BrandConfig
	if json.Unmarshal(data, &b) != nil {
		return nil
	}
	return &b
}

func saveBrandCache(code string, b *BrandConfig) {
	dir := brandCacheDir(code)
	_ = os.MkdirAll(dir, 0o755)
	data, _ := json.MarshalIndent(b, "", "  ")
	tmp := filepath.Join(dir, "brand.json.tmp")
	_ = os.WriteFile(tmp, data, 0o644)
	_ = os.Rename(tmp, filepath.Join(dir, "brand.json"))
}

// ---- persistence helpers ----

func loadPrefs() *Prefs {
	p := &Prefs{ClientPaths: make(map[string]string)}
	path := prefsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	_ = json.Unmarshal(data, p)
	if p.ClientPaths == nil {
		p.ClientPaths = make(map[string]string)
	}
	return p
}

func savePrefs(p *Prefs) error {
	path := prefsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	data, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func prefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".shiguang-launcher", "prefs.json")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
