//go:build integration

// Package pgtest starts disposable PostgreSQL containers for integration
// tests (build tag "integration", run with `make test-integration`). It needs
// a running Docker daemon.
package pgtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/postgres"
)

// Image is the PostgreSQL image used by tests. Keep it in sync with
// docker-compose.yml, so tests run against the same server version.
const Image = "postgres:16.15-alpine3.24"

// Start runs an empty PostgreSQL (no migrations applied) for the duration of
// the test and returns its connection URL. The container is removed when the
// test ends.
func Start(t testing.TB) string {
	t.Helper()
	// Not t.Context(): it is cancelled before cleanup functions run, and the
	// container must still be reachable to be terminated.
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("uptime"),
		tcpostgres.WithUsername("uptime"),
		tcpostgres.WithPassword("uptime"),
		// Waits for the second "ready to accept connections" log line: the
		// image's init script starts PostgreSQL once and restarts it.
		tcpostgres.BasicWaitStrategies(),
	)
	// Registered before the error check: a container that started but failed
	// its wait strategy must be removed too.
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err, "start postgres container (is Docker running?)")

	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return url
}

// NewPool starts PostgreSQL, applies all migrations and returns a connection
// pool to it: the starting point for repository tests.
func NewPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := Start(t)

	db, err := postgres.OpenDB(url)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	migrator, err := postgres.NewMigrator(db)
	require.NoError(t, err)
	_, err = migrator.Up(context.Background())
	require.NoError(t, err, "apply migrations")

	pool, err := postgres.NewPool(context.Background(), postgres.PoolConfig{URL: url, MaxConns: 4})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}
