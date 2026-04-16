// Package tenant defines the core multi-tenant data model for ShiguangCloud.
//
// Architecture: Row-Level Isolation (RLS) with tenant_id columns.
// Rationale: schema-per-tenant is overkill for <1000 tenants; RLS scales
// better with standard pgx pooling and avoids dynamic schema creation.
//
// Every table that holds tenant-specific data includes a `tenant_id UUID`
// column. All queries MUST filter by tenant_id — this is enforced by the
// repository layer (never construct raw SQL in handlers).
//
// Tenant hierarchy:
//   Tenant (operator) → ServerLine(s) → Player accounts
//                     → GateAgent(s)   → telemetry
//                     → LauncherTheme  → branding assets
//                     → Subscription   → billing tier
package tenant

import "time"

// Tenant is a private server operator — the paying customer of ShiguangCloud.
// Each tenant has their own workspace with server lines, gate agents, and player base.
type Tenant struct {
	ID          string    `json:"id" db:"id"`                    // UUID
	Name        string    `json:"name" db:"name"`                // display name ("决战永恒")
	Slug        string    `json:"slug" db:"slug"`                // URL-safe identifier ("juezhan-yongheng")
	Email       string    `json:"email" db:"email"`              // operator contact
	AdminHash   string    `json:"-" db:"admin_hash"`             // bcrypt hash for admin login
	Plan        Plan      `json:"plan" db:"plan"`                // free / pro / flagship
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	ExpiresAt   time.Time `json:"expires_at" db:"expires_at"`    // zero = no expiry (free plan)
	MaxPlayers  int       `json:"max_players" db:"max_players"`  // enforced by gate agent
	MaxLines    int       `json:"max_lines" db:"max_lines"`      // enforced at API
	Suspended   bool      `json:"suspended" db:"suspended"`      // billing or abuse suspension
}

// Plan tiers.
type Plan string

const (
	PlanFree     Plan = "free"
	PlanPro      Plan = "pro"
	PlanFlagship Plan = "flagship"
)

// ServerLine is one game server line configured by the tenant.
// A tenant on Pro plan might have 3 lines (e.g. "5.8 production", "4.8 test", "4.8 event").
type ServerLine struct {
	ID          string    `json:"id" db:"id"`          // UUID
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`      // "AionCore 5.8 主线"
	Version     string    `json:"version" db:"version"` // "5.8" or "4.8"
	AuthPort    int       `json:"auth_port" db:"auth_port"`
	GamePort    int       `json:"game_port" db:"game_port"`
	ChatPort    int       `json:"chat_port" db:"chat_port"`
	GameArgs    string    `json:"game_args" db:"game_args"`
	ClientPath  string    `json:"client_path" db:"client_path"`
	Enabled     bool      `json:"enabled" db:"enabled"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// GateAgent represents one shiguang-gate instance running on the tenant's
// server. It phones home to ShiguangHub via a WebSocket heartbeat channel.
type GateAgent struct {
	ID          string    `json:"id" db:"id"`          // UUID (auto-generated on first connect)
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	AgentKey    string    `json:"-" db:"agent_key"`     // pre-shared auth token
	Hostname    string    `json:"hostname" db:"hostname"`
	PublicIP    string    `json:"public_ip" db:"public_ip"` // gate's public IP (for launcher)
	LastSeen    time.Time `json:"last_seen" db:"last_seen"`
	Version     string    `json:"version" db:"version"` // gate binary version
	Status      string    `json:"status" db:"status"`   // "online" / "offline" / "degraded"
	AdminPort   int       `json:"admin_port" db:"admin_port"` // HTTP admin port on tenant's server
}

// LauncherTheme carries the branding assets for the tenant's white-label launcher.
// The launcher downloads this on first startup (or when the tenant code changes).
type LauncherTheme struct {
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	ServerName  string    `json:"server_name" db:"server_name"`  // "决战永恒" → shown in title bar
	LogoURL     string    `json:"logo_url" db:"logo_url"`        // URL to logo image
	BgURL       string    `json:"bg_url" db:"bg_url"`            // URL to background image
	AccentColor string    `json:"accent_color" db:"accent_color"` // hex color for primary button
	TextColor   string    `json:"text_color" db:"text_color"`     // hex color for text
	NewsURL     string    `json:"news_url" db:"news_url"`         // iframe URL for news panel
	PatchURL    string    `json:"patch_url" db:"patch_url"`       // manifest URL for patcher
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// PlayerAccount is the per-tenant player account table. Each tenant has their
// own player base — no cross-tenant account sharing.
type PlayerAccount struct {
	ID          string    `json:"id" db:"id"`
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	ServerLine  string    `json:"server_line" db:"server_line"` // server_line.id
	Name        string    `json:"name" db:"name"`
	PassHash    string    `json:"-" db:"pass_hash"`             // NCSoft hash or SHA1+Base64
	Email       string    `json:"email" db:"email"`
	BannedUntil *time.Time `json:"banned_until,omitempty" db:"banned_until"`
	BanReason   string    `json:"ban_reason,omitempty" db:"ban_reason"`
	LastLogin   time.Time `json:"last_login" db:"last_login"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// TenantCode is the short alphanumeric code a player enters in the launcher
// to connect to a specific tenant's server. Think of it as a "server invite code".
// Example: player enters "JUEZHAN" → launcher fetches branding for tenant "决战永恒".
type TenantCode struct {
	Code     string `json:"code" db:"code"`         // "JUEZHAN" (uppercase, 4-12 chars)
	TenantID string `json:"tenant_id" db:"tenant_id"`
	Active   bool   `json:"active" db:"active"`
}

// DailyStat holds one day's aggregate metrics for a tenant.
type DailyStat struct {
	TenantID    string    `json:"tenant_id" db:"tenant_id"`
	Date        string    `json:"date" db:"date"`           // YYYY-MM-DD
	TotalLogins int       `json:"total_logins" db:"total_logins"`
	NewAccounts int       `json:"new_accounts" db:"new_accounts"`
	PeakOnline  int       `json:"peak_online" db:"peak_online"`
	CreatedAt   time.Time `json:"-" db:"created_at"`
}

// SessionToken is the one-time auth token for the Token Handoff flow.
// Created by control when the launcher authenticates, consumed by the
// game server's auth handler within TTL.
type SessionToken struct {
	Token     string    `json:"token" db:"token"`       // UUID
	TenantID  string    `json:"tenant_id" db:"tenant_id"`
	Account   string    `json:"account" db:"account"`
	LineID    string    `json:"line_id" db:"line_id"`   // which server line
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"` // created_at + 300s
	Consumed  bool      `json:"consumed" db:"consumed"`
}
