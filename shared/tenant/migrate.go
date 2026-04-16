// migrate.go provides idempotent schema auto-migration for Hub startup.
//
// Uses CREATE TABLE IF NOT EXISTS so it's safe to run on every startup.
// No external migration tool needed — the schema is embedded in the binary.
//
// Design: We DON'T embed the original schema.sql (which uses CREATE TABLE
// without IF NOT EXISTS). Instead we maintain a separate idempotent DDL
// string. This avoids accidental data loss if the original schema evolves
// with ALTER statements.
package tenant

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// idempotent DDL — safe to run on every startup
const schemaDDL = `
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    email       TEXT NOT NULL,
    admin_hash  TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free'
                CHECK (plan IN ('free', 'pro', 'flagship')),
    max_players INT NOT NULL DEFAULT 50,
    max_lines   INT NOT NULL DEFAULT 1,
    suspended   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tenant_codes (
    code        TEXT PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    active      BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX IF NOT EXISTS idx_tenant_codes_tenant ON tenant_codes(tenant_id);

CREATE TABLE IF NOT EXISTS server_lines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    version     TEXT NOT NULL CHECK (version IN ('5.8', '4.8')),
    auth_port   INT NOT NULL DEFAULT 2108,
    game_port   INT NOT NULL DEFAULT 7777,
    chat_port   INT NOT NULL DEFAULT 0,
    game_args   TEXT NOT NULL DEFAULT '',
    client_path TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_server_lines_tenant ON server_lines(tenant_id);

CREATE TABLE IF NOT EXISTS gate_agents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_key   TEXT NOT NULL,
    hostname    TEXT NOT NULL DEFAULT '',
    public_ip   TEXT NOT NULL DEFAULT '',
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'offline'
                CHECK (status IN ('online', 'offline', 'degraded')),
    admin_port  INT NOT NULL DEFAULT 9090,
    UNIQUE(tenant_id, agent_key)
);
CREATE INDEX IF NOT EXISTS idx_gate_agents_tenant ON gate_agents(tenant_id);

CREATE TABLE IF NOT EXISTS launcher_themes (
    tenant_id    UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    server_name  TEXT NOT NULL DEFAULT '',
    logo_url     TEXT NOT NULL DEFAULT '',
    bg_url       TEXT NOT NULL DEFAULT '',
    accent_color TEXT NOT NULL DEFAULT '#4f8ef7',
    text_color   TEXT NOT NULL DEFAULT '#e6e8eb',
    news_url     TEXT NOT NULL DEFAULT '',
    patch_url    TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS player_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    server_line UUID REFERENCES server_lines(id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    pass_hash   TEXT NOT NULL,
    email       TEXT NOT NULL DEFAULT '',
    banned_until TIMESTAMPTZ,
    ban_reason  TEXT NOT NULL DEFAULT '',
    last_login  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_player_accounts_tenant ON player_accounts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_player_accounts_name ON player_accounts(tenant_id, name);

CREATE TABLE IF NOT EXISTS session_tokens (
    token       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account     TEXT NOT NULL,
    line_id     UUID REFERENCES server_lines(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '5 minutes'),
    consumed    BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS idx_session_tokens_lookup ON session_tokens(tenant_id, account, consumed)
    WHERE NOT consumed;

CREATE TABLE IF NOT EXISTS daily_stats (
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    date        DATE NOT NULL,
    peak_online INT NOT NULL DEFAULT 0,
    total_logins INT NOT NULL DEFAULT 0,
    new_accounts INT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, date)
);
`

// AutoMigrate executes idempotent CREATE TABLE IF NOT EXISTS statements.
// Safe to call on every startup — existing tables are not modified.
func AutoMigrate(ctx context.Context, pool *pgxpool.Pool) error {
	slog.Info("running schema auto-migration")
	_, err := pool.Exec(ctx, schemaDDL)
	if err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	slog.Info("schema auto-migration complete")
	return nil
}
