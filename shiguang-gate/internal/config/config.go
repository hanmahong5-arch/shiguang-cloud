// Package config loads gate YAML configuration. We deliberately keep the
// schema small: one "instance" per process (5.8 or 4.8), a list of routes,
// defense defaults, and admin HTTP binding.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level document for one gate instance.
//
// Example gate-58.yaml:
//
//	instance: "gate-58"
//	admin_http: "0.0.0.0:9090"
//	banlist_file: "bans-58.json"
//	defaults:
//	  max_conn_per_ip: 3
//	  rate_per_sec: 5
//	  burst: 10
//	  dial_timeout_ms: 5000
//	routes:
//	  - name: "5.8-auth"
//	    listen: "0.0.0.0:2108"
//	    upstream: "127.0.0.1:2108"
//	    proxy_protocol: true
//	  - name: "5.8-world"
//	    listen: "0.0.0.0:7777"
//	    upstream: "127.0.0.1:7777"
//	    proxy_protocol: true
type Config struct {
	// Instance is a human-readable name of this gate process (e.g. "gate-58").
	// It appears in logs and the admin API /status response.
	Instance string `yaml:"instance"`

	// AdminHTTP is the bind address for the HTTP admin API.
	AdminHTTP string `yaml:"admin_http"`

	// BanlistFile is the path where banned IPs are persisted.
	BanlistFile string `yaml:"banlist_file"`

	// Defaults apply to every route unless overridden.
	Defaults RouteDefaults `yaml:"defaults"`

	// Routes is the list of listen→upstream mappings.
	Routes []RouteConfig `yaml:"routes"`
}

// RouteDefaults are the fallback values for every route.
type RouteDefaults struct {
	MaxConnPerIP  int `yaml:"max_conn_per_ip"`
	RatePerSec    int `yaml:"rate_per_sec"`
	Burst         int `yaml:"burst"`
	DialTimeoutMS int `yaml:"dial_timeout_ms"`
}

// RouteConfig describes one listen→upstream mapping. Any zero-valued field
// falls back to Defaults.
type RouteConfig struct {
	Name          string `yaml:"name"`
	Listen        string `yaml:"listen"`
	Upstream      string `yaml:"upstream"`
	ProxyProtocol bool   `yaml:"proxy_protocol"`

	// Optional per-route overrides (0 = use defaults).
	MaxConnPerIP  int `yaml:"max_conn_per_ip"`
	RatePerSec    int `yaml:"rate_per_sec"`
	Burst         int `yaml:"burst"`
	DialTimeoutMS int `yaml:"dial_timeout_ms"`
}

// Load reads a YAML config file from path and validates it.
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
	if c.Instance == "" {
		return fmt.Errorf("instance must not be empty")
	}
	if c.AdminHTTP == "" {
		return fmt.Errorf("admin_http must not be empty")
	}
	if c.BanlistFile == "" {
		return fmt.Errorf("banlist_file must not be empty")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("at least one route required")
	}
	seen := make(map[string]bool)
	for i, r := range c.Routes {
		if r.Name == "" {
			return fmt.Errorf("route[%d]: name required", i)
		}
		if seen[r.Name] {
			return fmt.Errorf("route[%d]: duplicate name %q", i, r.Name)
		}
		seen[r.Name] = true
		if r.Listen == "" {
			return fmt.Errorf("route[%s]: listen required", r.Name)
		}
		if r.Upstream == "" {
			return fmt.Errorf("route[%s]: upstream required", r.Name)
		}
	}
	return nil
}

// Effective returns a route config with defaults applied.
func (r RouteConfig) Effective(d RouteDefaults) RouteConfig {
	if r.MaxConnPerIP == 0 {
		r.MaxConnPerIP = d.MaxConnPerIP
	}
	if r.RatePerSec == 0 {
		r.RatePerSec = d.RatePerSec
	}
	if r.Burst == 0 {
		r.Burst = d.Burst
	}
	if r.DialTimeoutMS == 0 {
		r.DialTimeoutMS = d.DialTimeoutMS
	}
	return r
}

// DialTimeout returns the dial timeout as a time.Duration.
func (r RouteConfig) DialTimeout() time.Duration {
	if r.DialTimeoutMS <= 0 {
		return 5 * time.Second
	}
	return time.Duration(r.DialTimeoutMS) * time.Millisecond
}
