package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestConfig_LoadValid58(t *testing.T) {
	yaml := `
instance: gate-58
admin_http: 127.0.0.1:9090
banlist_file: bans.json
defaults:
  max_conn_per_ip: 3
  rate_per_sec: 5
  burst: 10
  dial_timeout_ms: 5000
routes:
  - name: "5.8-auth"
    listen: "0.0.0.0:2108"
    upstream: "127.0.0.1:2108"
    proxy_protocol: true
  - name: "5.8-world"
    listen: "0.0.0.0:7777"
    upstream: "127.0.0.1:7777"
    proxy_protocol: true
`
	path := writeFile(t, "gate-58.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Instance != "gate-58" {
		t.Errorf("instance = %q", cfg.Instance)
	}
	if len(cfg.Routes) != 2 {
		t.Errorf("routes=%d want 2", len(cfg.Routes))
	}
	if !cfg.Routes[0].ProxyProtocol {
		t.Error("5.8-auth should have proxy_protocol=true")
	}
}

func TestConfig_LoadValid48(t *testing.T) {
	yaml := `
instance: gate-48
admin_http: 127.0.0.1:9091
banlist_file: bans48.json
defaults:
  max_conn_per_ip: 3
  rate_per_sec: 5
  burst: 10
  dial_timeout_ms: 5000
routes:
  - name: "4.8-login"
    listen: "0.0.0.0:2107"
    upstream: "127.0.0.1:2107"
    proxy_protocol: true
  - name: "4.8-game"
    listen: "0.0.0.0:7778"
    upstream: "127.0.0.1:7778"
    proxy_protocol: true
  - name: "4.8-chat"
    listen: "0.0.0.0:10241"
    upstream: "127.0.0.1:10241"
    proxy_protocol: true
`
	path := writeFile(t, "gate-48.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Routes) != 3 {
		t.Errorf("routes=%d want 3", len(cfg.Routes))
	}
}

func TestConfig_RejectDuplicateRouteName(t *testing.T) {
	yaml := `
instance: gate-58
admin_http: 127.0.0.1:9090
banlist_file: bans.json
routes:
  - name: "dup"
    listen: "0.0.0.0:1000"
    upstream: "127.0.0.1:1000"
  - name: "dup"
    listen: "0.0.0.0:2000"
    upstream: "127.0.0.1:2000"
`
	path := writeFile(t, "dup.yaml", yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error on duplicate route name")
	}
}

func TestConfig_RejectEmptyRoutes(t *testing.T) {
	yaml := `
instance: gate
admin_http: 127.0.0.1:9090
banlist_file: bans.json
routes: []
`
	path := writeFile(t, "empty.yaml", yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error on empty routes")
	}
}

func TestConfig_EffectiveAppliesDefaults(t *testing.T) {
	defaults := RouteDefaults{
		MaxConnPerIP:  5,
		RatePerSec:    10,
		Burst:         20,
		DialTimeoutMS: 3000,
	}
	route := RouteConfig{Name: "x", Listen: "0:1", Upstream: "0:2"}
	eff := route.Effective(defaults)
	if eff.MaxConnPerIP != 5 {
		t.Errorf("max_conn_per_ip = %d", eff.MaxConnPerIP)
	}
	if eff.RatePerSec != 10 {
		t.Errorf("rate_per_sec = %d", eff.RatePerSec)
	}
	if eff.DialTimeout().Milliseconds() != 3000 {
		t.Errorf("dial timeout = %v", eff.DialTimeout())
	}
}
