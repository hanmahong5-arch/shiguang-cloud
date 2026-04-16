// Package tenant provides the multi-tenant CRUD repository for ShiguangCloud.
//
// Every query method takes tenant_id as the first parameter to enforce
// row-level isolation. There is NO public method that queries without
// tenant_id (except tenant creation and code lookup which are root operations).
//
// All queries use pgx parameterized placeholders ($1, $2, …) — NEVER
// string concatenation — categorically eliminating SQL injection.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is the data access layer for the multi-tenant schema.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo creates a repository backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// ---- Tenant CRUD ----

// CreateTenant inserts a new tenant (operator). Returns the generated UUID.
func (r *Repo) CreateTenant(ctx context.Context, t *Tenant) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, email, admin_hash, plan, max_players, max_lines)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id`,
		t.Name, t.Slug, t.Email, t.AdminHash, t.Plan, t.MaxPlayers, t.MaxLines,
	).Scan(&id)
	return id, err
}

// GetTenant fetches a tenant by ID.
func (r *Repo) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	t := &Tenant{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, email, admin_hash, plan, max_players, max_lines,
		        suspended, created_at, expires_at
		 FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.Email, &t.AdminHash, &t.Plan,
		&t.MaxPlayers, &t.MaxLines, &t.Suspended, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("tenant %s not found", id)
	}
	return t, err
}

// GetTenantBySlug fetches a tenant by URL slug.
func (r *Repo) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	t := &Tenant{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, email, admin_hash, plan, max_players, max_lines,
		        suspended, created_at, expires_at
		 FROM tenants WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.Email, &t.AdminHash, &t.Plan,
		&t.MaxPlayers, &t.MaxLines, &t.Suspended, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("tenant slug %q not found", slug)
	}
	return t, err
}

// GetTenantByEmail fetches a tenant by email address (for login).
func (r *Repo) GetTenantByEmail(ctx context.Context, email string) (*Tenant, error) {
	t := &Tenant{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, slug, email, admin_hash, plan, max_players, max_lines,
		        suspended, created_at, expires_at
		 FROM tenants WHERE email = $1`, email,
	).Scan(&t.ID, &t.Name, &t.Slug, &t.Email, &t.AdminHash, &t.Plan,
		&t.MaxPlayers, &t.MaxLines, &t.Suspended, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("tenant email %q not found", email)
	}
	return t, err
}

// UpdateTenantPassword updates the admin password hash for a tenant.
func (r *Repo) UpdateTenantPassword(ctx context.Context, tenantID, newHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE tenants SET admin_hash = $2 WHERE id = $1`,
		tenantID, newHash)
	return err
}

// ---- Tenant Codes ----

// ResolveTenantCode looks up an active invite code and returns the tenant_id.
func (r *Repo) ResolveTenantCode(ctx context.Context, code string) (string, error) {
	var tenantID string
	err := r.pool.QueryRow(ctx,
		`SELECT tenant_id FROM tenant_codes WHERE code = $1 AND active = TRUE`, code,
	).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("invite code %q not found or inactive", code)
	}
	return tenantID, err
}

// CreateTenantCode adds an invite code for a tenant.
func (r *Repo) CreateTenantCode(ctx context.Context, code, tenantID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO tenant_codes (code, tenant_id) VALUES ($1, $2)`, code, tenantID)
	return err
}

// ListTenantCodes returns all invite codes for a tenant.
func (r *Repo) ListTenantCodes(ctx context.Context, tenantID string) ([]TenantCode, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT code, tenant_id, active FROM tenant_codes WHERE tenant_id = $1 ORDER BY code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []TenantCode
	for rows.Next() {
		var c TenantCode
		if err := rows.Scan(&c.Code, &c.TenantID, &c.Active); err != nil {
			return nil, err
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// ---- Server Lines ----

// ListServerLines returns all enabled server lines for a tenant.
func (r *Repo) ListServerLines(ctx context.Context, tenantID string) ([]ServerLine, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, name, version, auth_port, game_port, chat_port,
		        game_args, client_path, enabled, created_at
		 FROM server_lines WHERE tenant_id = $1 AND enabled = TRUE
		 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []ServerLine
	for rows.Next() {
		var l ServerLine
		if err := rows.Scan(&l.ID, &l.TenantID, &l.Name, &l.Version, &l.AuthPort,
			&l.GamePort, &l.ChatPort, &l.GameArgs, &l.ClientPath, &l.Enabled, &l.CreatedAt); err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// CreateServerLine adds a server line for a tenant (enforces max_lines limit).
// Uses pg_advisory_xact_lock to serialize concurrent inserts per tenant,
// preventing a TOCTOU race where two requests both see count < max.
func (r *Repo) CreateServerLine(ctx context.Context, l *ServerLine) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Advisory lock keyed on tenant_id hash — serializes per-tenant line creation.
	// Released automatically when the transaction commits or rolls back.
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, l.TenantID)
	if err != nil {
		return "", fmt.Errorf("advisory lock: %w", err)
	}

	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO server_lines (tenant_id, name, version, auth_port, game_port, chat_port, game_args, client_path)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8
		 WHERE (SELECT COUNT(*) FROM server_lines WHERE tenant_id = $1 AND enabled = TRUE)
		       < (SELECT max_lines FROM tenants WHERE id = $1)
		 RETURNING id`,
		l.TenantID, l.Name, l.Version, l.AuthPort, l.GamePort, l.ChatPort, l.GameArgs, l.ClientPath,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("max server lines reached for tenant %s", l.TenantID)
	}
	if err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

// UpdateServerLine updates a server line's mutable fields.
// Only updates fields that the operator is allowed to change.
func (r *Repo) UpdateServerLine(ctx context.Context, tenantID, lineID string, name, version string, authPort, gamePort, chatPort int) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE server_lines
		 SET name = $3, version = $4, auth_port = $5, game_port = $6, chat_port = $7
		 WHERE id = $2 AND tenant_id = $1 AND enabled = TRUE`,
		tenantID, lineID, name, version, authPort, gamePort, chatPort)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server line %s not found or disabled", lineID)
	}
	return nil
}

// DeleteServerLine soft-deletes a server line by setting enabled=false.
// Hard delete is intentionally avoided — agents referencing this line
// need time to detect the change via config refresh.
func (r *Repo) DeleteServerLine(ctx context.Context, tenantID, lineID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE server_lines SET enabled = FALSE
		 WHERE id = $2 AND tenant_id = $1 AND enabled = TRUE`,
		tenantID, lineID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server line %s not found or already disabled", lineID)
	}
	return nil
}

// DeleteTenantCode removes an invite code (hard delete).
func (r *Repo) DeleteTenantCode(ctx context.Context, tenantID, code string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM tenant_codes WHERE code = $2 AND tenant_id = $1`,
		tenantID, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite code %q not found", code)
	}
	return nil
}

// ---- Gate Agents ----

// UpsertGateAgent registers or updates a gate agent's heartbeat.
func (r *Repo) UpsertGateAgent(ctx context.Context, a *GateAgent) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO gate_agents (tenant_id, agent_key, hostname, public_ip, version, status, admin_port, last_seen)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (tenant_id, agent_key) DO UPDATE SET
		   hostname = $3, public_ip = $4, version = $5, status = $6, admin_port = $7, last_seen = now()`,
		a.TenantID, a.AgentKey, a.Hostname, a.PublicIP, a.Version, a.Status, a.AdminPort)
	return err
}

// ListGateAgents returns all gate agents for a tenant.
func (r *Repo) ListGateAgents(ctx context.Context, tenantID string) ([]GateAgent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, tenant_id, agent_key, hostname, public_ip, last_seen, version, status, admin_port
		 FROM gate_agents WHERE tenant_id = $1 ORDER BY last_seen DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []GateAgent
	for rows.Next() {
		var a GateAgent
		if err := rows.Scan(&a.ID, &a.TenantID, &a.AgentKey, &a.Hostname, &a.PublicIP,
			&a.LastSeen, &a.Version, &a.Status, &a.AdminPort); err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// GetOnlineGateIP returns the public IP of the most recently seen online agent
// for a tenant. Returns empty string if no online agents exist.
// An agent is considered "online" if its status is 'online' and
// last_seen is within 60 seconds.
func (r *Repo) GetOnlineGateIP(ctx context.Context, tenantID string) string {
	var ip string
	err := r.pool.QueryRow(ctx,
		`SELECT public_ip FROM gate_agents
		 WHERE tenant_id = $1 AND status = 'online'
		   AND last_seen > now() - interval '60 seconds'
		 ORDER BY last_seen DESC LIMIT 1`, tenantID,
	).Scan(&ip)
	if err != nil {
		return ""
	}
	return ip
}

// GetGateAgentByKey validates an agent key and returns the agent + tenant_id.
func (r *Repo) GetGateAgentByKey(ctx context.Context, agentKey string) (*GateAgent, error) {
	a := &GateAgent{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, agent_key, hostname, public_ip, last_seen, version, status, admin_port
		 FROM gate_agents WHERE agent_key = $1`, agentKey,
	).Scan(&a.ID, &a.TenantID, &a.AgentKey, &a.Hostname, &a.PublicIP,
		&a.LastSeen, &a.Version, &a.Status, &a.AdminPort)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("gate agent key not found")
	}
	return a, err
}

// ---- Launcher Theme ----

// GetTheme returns the launcher branding for a tenant.
func (r *Repo) GetTheme(ctx context.Context, tenantID string) (*LauncherTheme, error) {
	th := &LauncherTheme{}
	err := r.pool.QueryRow(ctx,
		`SELECT tenant_id, server_name, logo_url, bg_url, accent_color, text_color,
		        news_url, patch_url, updated_at
		 FROM launcher_themes WHERE tenant_id = $1`, tenantID,
	).Scan(&th.TenantID, &th.ServerName, &th.LogoURL, &th.BgURL, &th.AccentColor,
		&th.TextColor, &th.NewsURL, &th.PatchURL, &th.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("theme not found for tenant %s", tenantID)
	}
	return th, err
}

// UpsertTheme creates or updates the launcher theme for a tenant.
func (r *Repo) UpsertTheme(ctx context.Context, th *LauncherTheme) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO launcher_themes (tenant_id, server_name, logo_url, bg_url, accent_color, text_color, news_url, patch_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (tenant_id) DO UPDATE SET
		   server_name = $2, logo_url = $3, bg_url = $4, accent_color = $5,
		   text_color = $6, news_url = $7, patch_url = $8, updated_at = now()`,
		th.TenantID, th.ServerName, th.LogoURL, th.BgURL, th.AccentColor,
		th.TextColor, th.NewsURL, th.PatchURL)
	return err
}

// ---- Session Tokens ----

// CreateSessionToken creates a one-time token for Token Handoff.
func (r *Repo) CreateSessionToken(ctx context.Context, tenantID, account, lineID string) (string, error) {
	var token string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO session_tokens (tenant_id, account, line_id)
		 VALUES ($1, $2, $3)
		 RETURNING token`,
		tenantID, account, lineID,
	).Scan(&token)
	return token, err
}

// ConsumeSessionToken validates and consumes a session token. Returns the
// account name if valid, or an error if expired/consumed/not found.
func (r *Repo) ConsumeSessionToken(ctx context.Context, tenantID, token string) (account string, lineID string, err error) {
	err = r.pool.QueryRow(ctx,
		`UPDATE session_tokens SET consumed = TRUE
		 WHERE token = $1 AND tenant_id = $2 AND consumed = FALSE AND expires_at > now()
		 RETURNING account, line_id`,
		token, tenantID,
	).Scan(&account, &lineID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("session token invalid, expired, or already consumed")
	}
	return
}

// PurgeExpiredTokens removes consumed/expired tokens older than 1 hour.
func (r *Repo) PurgeExpiredTokens(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM session_tokens WHERE expires_at < now() - interval '1 hour'`)
	return tag.RowsAffected(), err
}

// ---- Player Accounts ----

// CreatePlayer inserts a player account under a tenant.
func (r *Repo) CreatePlayer(ctx context.Context, p *PlayerAccount) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO player_accounts (tenant_id, server_line, name, pass_hash, email)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		p.TenantID, p.ServerLine, p.Name, p.PassHash, p.Email,
	).Scan(&id)
	return id, err
}

// GetPlayerByName finds a player account by name within a tenant.
func (r *Repo) GetPlayerByName(ctx context.Context, tenantID, name string) (*PlayerAccount, error) {
	p := &PlayerAccount{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, tenant_id, server_line, name, pass_hash, email,
		        banned_until, ban_reason, last_login, created_at
		 FROM player_accounts WHERE tenant_id = $1 AND name = $2`,
		tenantID, name,
	).Scan(&p.ID, &p.TenantID, &p.ServerLine, &p.Name, &p.PassHash, &p.Email,
		&p.BannedUntil, &p.BanReason, &p.LastLogin, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // not found is a normal case, not an error
	}
	return p, err
}

// UpdatePlayerPassword changes a player's password hash.
func (r *Repo) UpdatePlayerPassword(ctx context.Context, tenantID, name, oldHash, newHash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE player_accounts SET pass_hash = $4
		 WHERE tenant_id = $1 AND name = $2 AND pass_hash = $3`,
		tenantID, name, oldHash, newHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("password mismatch")
	}
	return nil
}

// BanPlayer sets ban_until and ban_reason for a player.
func (r *Repo) BanPlayer(ctx context.Context, tenantID, name, reason string, until time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE player_accounts SET banned_until = $3, ban_reason = $4
		 WHERE tenant_id = $1 AND name = $2`,
		tenantID, name, until, reason)
	return err
}

// UnbanPlayer clears the ban.
func (r *Repo) UnbanPlayer(ctx context.Context, tenantID, name string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE player_accounts SET banned_until = NULL, ban_reason = ''
		 WHERE tenant_id = $1 AND name = $2`,
		tenantID, name)
	return err
}

// RecordLogin updates last_login timestamp.
func (r *Repo) RecordLogin(ctx context.Context, tenantID, name string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE player_accounts SET last_login = now() WHERE tenant_id = $1 AND name = $2`,
		tenantID, name)
	return err
}

// ---- Daily Stats ----

// IncrDailyStats atomically increments today's counters.
func (r *Repo) IncrDailyStats(ctx context.Context, tenantID string, logins, newAccounts int, peakOnline int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO daily_stats (tenant_id, date, total_logins, new_accounts, peak_online)
		 VALUES ($1, CURRENT_DATE, $2, $3, $4)
		 ON CONFLICT (tenant_id, date) DO UPDATE SET
		   total_logins = daily_stats.total_logins + $2,
		   new_accounts = daily_stats.new_accounts + $3,
		   peak_online = GREATEST(daily_stats.peak_online, $4)`,
		tenantID, logins, newAccounts, peakOnline)
	return err
}

// ListDailyStats returns the last N days of stats for a tenant.
// Results are ordered newest-first. The `days` parameter controls the range.
func (r *Repo) ListDailyStats(ctx context.Context, tenantID string, days int) ([]DailyStat, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	rows, err := r.pool.Query(ctx,
		`SELECT tenant_id, date::text, total_logins, new_accounts, peak_online
		 FROM daily_stats
		 WHERE tenant_id = $1 AND date >= CURRENT_DATE - $2::int
		 ORDER BY date DESC`, tenantID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []DailyStat
	for rows.Next() {
		var s DailyStat
		if err := rows.Scan(&s.TenantID, &s.Date, &s.TotalLogins, &s.NewAccounts, &s.PeakOnline); err != nil {
			return nil, err
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}

// RotateAgentKey generates a new agent_key for a gate agent.
// The old key is immediately invalidated. Returns the new key.
func (r *Repo) RotateAgentKey(ctx context.Context, tenantID, agentID string) (string, error) {
	var newKey string
	err := r.pool.QueryRow(ctx,
		`UPDATE gate_agents
		 SET agent_key = encode(gen_random_bytes(32), 'hex')
		 WHERE id = $2 AND tenant_id = $1
		 RETURNING agent_key`,
		tenantID, agentID).Scan(&newKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("agent %s not found", agentID)
	}
	return newKey, err
}
