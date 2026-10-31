//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/postgres/pgtest"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/postgres"
)

func TestUsers(t *testing.T) {
	users := postgres.NewUsers(pgtest.NewPool(t))
	ctx := t.Context()

	created, err := users.Create(ctx, "alice@example.com", "hash-1")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID, "the database generates the ID")
	assert.Equal(t, "alice@example.com", created.Email)
	assert.Equal(t, "hash-1", created.PasswordHash)
	assert.WithinDuration(t, time.Now(), created.CreatedAt, time.Minute)

	t.Run("get by email", func(t *testing.T) {
		got, err := users.GetByEmail(ctx, "alice@example.com")
		require.NoError(t, err)
		assert.Equal(t, created, got)
	})

	t.Run("get by id", func(t *testing.T) {
		got, err := users.GetByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created, got)
	})

	t.Run("duplicate email", func(t *testing.T) {
		_, err := users.Create(ctx, "alice@example.com", "hash-2")
		require.ErrorIs(t, err, domain.ErrConflict)
	})

	t.Run("unknown email", func(t *testing.T) {
		_, err := users.GetByEmail(ctx, "bob@example.com")
		require.ErrorIs(t, err, domain.ErrNotFound)
	})

	t.Run("unknown id", func(t *testing.T) {
		_, err := users.GetByID(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrNotFound)
	})
}
