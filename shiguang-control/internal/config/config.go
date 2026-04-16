// Package config loads shiguang-control YAML configuration.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/shiguang/shared/tenant"
)

// Config is the top-level YAML for shiguang-control.
type Config struct {
	// Bind specifies the HTTP/HTTPS listen address for the control API +
	// Admin SPA static hosting. Typically "0.0.0.0:10443".
	Bind string `yaml:"bind"`

	// TLS, if non-nil, enables HTTPS. Leave empty for plain HTTP (dev only).
	TLS *TLSConfig `yaml:"tls,omitempty"`

	// JWT configures admin authentication. Never commit the secret.
	JWT JWTConfig `yaml:"jwt"`

	// Database connection strings for the two service lines.
	DB58 string `yaml:"db_58"` // pgx-style DSN for aion_world_live
	DB48 string `yaml:"db_48"` // pgx-style DSN for al_server_ls

	// Gates lists the gate admin HTTP endpoints this control instance manages.
	// Used by /api/admin/ban to propagate to all gate processes.
	Gates []GateEndpoint `yaml:"gates"`

	// Launcher is what the control server hands out to launchers via
	// /api/launcher/config when they connect. Operators hot-edit these via
	// the admin SPA; no restart required.
	Launcher tenant.LauncherWireConfig `yaml:"launcher"`
}

// TLSConfig points at PEM-encoded cert/key files.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// JWTConfig controls admin JWT signing.
type JWTConfig struct {
	Secret  string `yaml:"secret"`   // HS256 signing key
	Issuer  string `yaml:"issuer"`   // iss claim
	TTLDays int    `yaml:"ttl_days"` // token lifetime
}

// GateEndpoint is one gate admin API address.
type GateEndpoint struct {
	Name string `yaml:"name"` // human-readable ("gate-58", "gate-48")
	URL  string `yaml:"url"`  // "http://127.0.0.1:9090"
}

// Load reads YAML config from path and applies defensive validation.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Bind == "" {
		return fmt.Errorf("bind required")
	}
	if c.JWT.Secret == "" || len(c.JWT.Secret) < 16 {
		return fmt.Errorf("jwt.secret must be at least 16 characters")
	}
	if c.JWT.TTLDays <= 0 {
		c.JWT.TTLDays = 7
	}
	if c.JWT.Issuer == "" {
		c.JWT.Issuer = "shiguang-control"
	}
	if c.Launcher.PublicGateIP == "" {
		return fmt.Errorf("launcher.public_gate_ip required")
	}
	if len(c.Launcher.Servers) == 0 {
		return fmt.Errorf("launcher.servers must not be empty")
	}
	return nil
}
