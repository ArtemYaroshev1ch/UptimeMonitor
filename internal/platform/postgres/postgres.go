// Package postgres opens PostgreSQL connections and applies schema migrations.
package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// PoolConfig configures the connection pool.
type PoolConfig struct {
	// URL is a libpq connection string or URL (contains the password).
	URL      string
	MaxConns int32
}

// NewPool returns a connection pool. It does not connect: connections are
// opened on first use, so the process starts even while the database is down
// and reports that through /readyz instead of crash-looping. Close the pool
// when done.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	// pgx redacts the password in parse errors, so the error is safe to log.
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DATABASE_URL: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	return pool, nil
}

// OpenDB returns a database/sql handle backed by pgx. Only migrations use it:
// goose works with *sql.DB, the application code uses the pgx pool.
func OpenDB(url string) (*sql.DB, error) {
	connCfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DATABASE_URL: %w", err)
	}
	return stdlib.OpenDB(*connCfg), nil
}
