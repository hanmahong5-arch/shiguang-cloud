// Package aiondb provides a thin pgx pool wrapper for the two AION database
// schemas (NCSoft-style aion_world_live for 5.8, AL-Aion al_server_ls for 4.8).
// It intentionally stays tiny — no ORM, no migrations — so the control server
// can reach for parameterized queries directly.
package aiondb

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a pgxpool.Pool with a friendly constructor and safe defaults.
type Pool struct {
	*pgxpool.Pool
}

// Open parses dsn and constructs a connection pool with defensive limits.
// The pool is verified via Ping() before returning.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	// Safe defaults: small pool, short idle, connect timeout to fail fast.
	cfg.MaxConns = 16
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.MaxConnIdleTime = 10 * time.Minute
	cfg.HealthCheckPeriod = 1 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool new: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Pool{Pool: pool}, nil
}

// Close releases all connections.
func (p *Pool) Close() {
	if p.Pool != nil {
		p.Pool.Close()
	}
}
