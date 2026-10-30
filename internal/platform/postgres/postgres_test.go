package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// badURL has an invalid port, so parsing fails.
const badURL = "postgres://uptime:supersecret@localhost:notaport/uptime"

func TestParseErrorsHidePassword(t *testing.T) {
	t.Parallel()

	_, poolErr := NewPool(t.Context(), PoolConfig{URL: badURL, MaxConns: 1})
	_, dbErr := OpenDB(badURL)

	for name, err := range map[string]error{"NewPool": poolErr, "OpenDB": dbErr} {
		require.Error(t, err, name)
		assert.ErrorContains(t, err, "DATABASE_URL", name)
		// The error ends up in logs.
		assert.NotContains(t, err.Error(), "supersecret", name)
	}
}

// The pool is lazy: creating it succeeds without a database, and Ping
// reports the problem within the caller's deadline instead of hanging.
func TestPoolWithoutDatabase(t *testing.T) {
	t.Parallel()

	// Nothing listens on port 1.
	pool, err := NewPool(t.Context(), PoolConfig{
		URL:      "postgres://uptime:uptime@127.0.0.1:1/uptime?sslmode=disable",
		MaxConns: 1,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Connection refused takes about 2 s on Windows; the deadline bounds it.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = pool.Ping(ctx)

	require.Error(t, err)
	assert.False(t, errors.Is(err, context.DeadlineExceeded), "fails fast instead of hanging: %v", err)
}
