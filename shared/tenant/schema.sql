-- ShiguangCloud multi-tenant schema (PostgreSQL 16+).
-- Row-Level Isolation model: every tenant-scoped table has tenant_id.
-- Deploy into a dedicated database: shiguang_cloud.
--
-- Convention: UUIDs for all primary keys (gen_random_uuid()).
-- Convention: all timestamps are timestamptz (UTC).

CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- for gen_random_uuid()

-- ==================== Tenant (operator) ====================

CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,               -- URL-safe, lowercase
    email       TEXT NOT NULL,
    admin_hash  TEXT NOT NULL,                       -- bcrypt
    plan        TEXT NOT NULL DEFAULT 'free'         -- free / pro / flagship
                CHECK (plan IN ('free', 'pro', 'flagship')),
    max_players INT NOT NULL DEFAULT 50,
    max_lines   INT NOT NULL DEFAULT 1,
    suspended   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ                          -- NULL = no expiry
);

-- ==================== Tenant invite codes ====================
-- Players enter this code in the launcher to connect to a tenant's server.

CREATE TABLE tenant_codes (
    code        TEXT PRIMARY KEY,                    -- e.g. "JUEZHAN"
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    active      BOOLEAN NOT NULL DEFAULT TRUE
);
CREATE INDEX idx_tenant_codes_tenant ON tenant_codes(tenant_id);

-- ==================== Server lines ====================

CREATE TABLE server_lines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    version     TEXT NOT NULL CHECK (version IN ('5.8', '4.8')),
    auth_port   INT NOT NULL DEFAULT 2108,
    game_port   INT NOT NULL DEFAULT 7777,
    chat_port   INT NOT NULL DEFAULT 0,              -- 0 = none
    game_args   TEXT NOT NULL DEFAULT '',
    client_path TEXT NOT NULL DEFAULT '',
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name)
);
CREATE INDEX idx_server_lines_tenant ON server_lines(tenant_id);

-- ==================== Gate agents ====================
-- Each gate agent running on a tenant's server registers here via heartbeat.

CREATE TABLE gate_agents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_key   TEXT NOT NULL,                       -- pre-shared bearer token
    hostname    TEXT NOT NULL DEFAULT '',
    public_ip   TEXT NOT NULL DEFAULT '',
    last_seen   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'offline'
                CHECK (status IN ('online', 'offline', 'degraded')),
    admin_port  INT NOT NULL DEFAULT 9090,
    UNIQUE(tenant_id, agent_key)
);
CREATE INDEX idx_gate_agents_tenant ON gate_agents(tenant_id);

-- ==================== Launcher themes (branding) ====================

CREATE TABLE launcher_themes (
    tenant_id    UUID PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    server_name  TEXT NOT NULL DEFAULT '',            -- title bar text
    logo_url     TEXT NOT NULL DEFAULT '',
    bg_url       TEXT NOT NULL DEFAULT '',
    accent_color TEXT NOT NULL DEFAULT '#4f8ef7',
    text_color   TEXT NOT NULL DEFAULT '#e6e8eb',
    news_url     TEXT NOT NULL DEFAULT '',
    patch_url    TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ==================== Player accounts (per-tenant) ====================
-- This replaces the game server's own account_data table for the launcher's
-- pre-authentication flow. The actual game account creation still happens
-- on the game server's database via the auth protocol.

CREATE TABLE player_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    server_line UUID REFERENCES server_lines(id) ON DELETE SET NULL,
    name        TEXT NOT NULL,
    pass_hash   TEXT NOT NULL,                       -- NCSoft hash or SHA1+Base64
    email       TEXT NOT NULL DEFAULT '',
    banned_until TIMESTAMPTZ,
    ban_reason  TEXT NOT NULL DEFAULT '',
    last_login  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, name)                          -- account names unique per tenant
);
CREATE INDEX idx_player_accounts_tenant ON player_accounts(tenant_id);
CREATE INDEX idx_player_accounts_name ON player_accounts(tenant_id, name);

-- ==================== Session tokens (Token Handoff) ====================
-- One-time tokens created by control, consumed by game server auth handler.

CREATE TABLE session_tokens (
    token       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account     TEXT NOT NULL,
    line_id     UUID REFERENCES server_lines(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '5 minutes'),
    consumed    BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_session_tokens_lookup ON session_tokens(tenant_id, account, consumed)
    WHERE NOT consumed;

-- Auto-purge expired tokens (run via pg_cron or application-level sweeper)
-- DELETE FROM session_tokens WHERE expires_at < now() - interval '1 hour';

-- ==================== Analytics (lightweight) ====================

CREATE TABLE daily_stats (
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    date        DATE NOT NULL,
    peak_online INT NOT NULL DEFAULT 0,
    total_logins INT NOT NULL DEFAULT 0,
    new_accounts INT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, date)
);
