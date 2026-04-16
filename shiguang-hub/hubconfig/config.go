// Package hubconfig loads the ShiguangHub YAML configuration.
package hubconfig

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Hub config.
type Config struct {
	Bind     string `yaml:"bind"`      // HTTP listen address (e.g. "0.0.0.0:443")
	GRPCBind string `yaml:"grpc_bind"` // gRPC listen (e.g. "0.0.0.0:50051")
	DSN      string `yaml:"dsn"`       // PostgreSQL DSN for shiguang_cloud DB
	JWT      struct {
		Secret  string `yaml:"secret"`
		Issuer  string `yaml:"issuer"`
		TTLDays int    `yaml:"ttl_days"`
	} `yaml:"jwt"`
	// PatchDir is the root directory for hosting patch files (optional).
	// Structure: patch_dir/{TENANT_CODE}/chunk-manifest.json + chunks/{hash}
	// Operators run patchbuilder → output to patch_dir/{CODE}/ → Hub serves them.
	PatchDir string `yaml:"patch_dir,omitempty"`
	// TLS (optional; omit for dev HTTP)
	TLS *struct {
		CertFile string `yaml:"cert_file"`
		KeyFile  string `yaml:"key_file"`
	} `yaml:"tls,omitempty"`
}

// Load parses YAML config from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Bind == "" {
		cfg.Bind = "0.0.0.0:10443"
	}
	if cfg.JWT.Secret == "" || len(cfg.JWT.Secret) < 16 {
		return nil, fmt.Errorf("jwt.secret must be >= 16 characters")
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = "shiguang-hub"
	}
	if cfg.JWT.TTLDays <= 0 {
		cfg.JWT.TTLDays = 30
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("dsn required")
	}
	return &cfg, nil
}
