package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "control.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestConfig_LoadValid(t *testing.T) {
	yaml := `
bind: "0.0.0.0:10443"
jwt:
  secret: "a_very_long_secret_string_123"
  issuer: "test"
  ttl_days: 7
db_58: "postgres://x"
db_48: "postgres://y"
gates:
  - name: gate-58
    url: http://127.0.0.1:9090
launcher:
  public_gate_ip: "1.2.3.4"
  patch_manifest_url: "https://x/manifest.json"
  servers:
    - id: "5.8"
      name: "5.8"
      auth_port: 2108
      game_args: ""
      client_path: ""
`
	path := writeFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Bind != "0.0.0.0:10443" {
		t.Errorf("bind=%q", cfg.Bind)
	}
	if len(cfg.Launcher.Servers) != 1 {
		t.Errorf("servers=%d", len(cfg.Launcher.Servers))
	}
}

func TestConfig_ShortSecretRejected(t *testing.T) {
	yaml := `
bind: "0.0.0.0:10443"
jwt:
  secret: "short"
  ttl_days: 7
launcher:
  public_gate_ip: "1.2.3.4"
  servers:
    - id: "5.8"
      name: "5.8"
      auth_port: 2108
`
	path := writeFile(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error on short secret")
	}
}

func TestConfig_MissingGateIPRejected(t *testing.T) {
	yaml := `
bind: "0.0.0.0:10443"
jwt:
  secret: "a_very_long_secret_string_123"
  ttl_days: 7
launcher:
  public_gate_ip: ""
  servers:
    - id: "5.8"
      name: "5.8"
      auth_port: 2108
`
	path := writeFile(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Error("expected error on missing gate IP")
	}
}

func TestConfig_Defaults(t *testing.T) {
	yaml := `
bind: "0.0.0.0:10443"
jwt:
  secret: "a_very_long_secret_string_123"
launcher:
  public_gate_ip: "1.2.3.4"
  servers:
    - id: "5.8"
      name: "5.8"
      auth_port: 2108
`
	path := writeFile(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.JWT.TTLDays != 7 {
		t.Errorf("ttl_days default = %d", cfg.JWT.TTLDays)
	}
	if cfg.JWT.Issuer != "shiguang-control" {
		t.Errorf("issuer default = %q", cfg.JWT.Issuer)
	}
}
