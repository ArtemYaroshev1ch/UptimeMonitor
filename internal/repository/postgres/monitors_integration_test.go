//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/postgres/pgtest"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/postgres"
)

func settings(name string) domain.MonitorSettings {
	s := domain.DefaultMonitorSettings()
	s.Name = name
	s.URL = "https://example.com/" + name
	return s
}

func TestMonitorsCRUD(t *testing.T) {
	pool := pgtest.NewPool(t)
	users := postgres.NewUsers(pool)
	monitors := postgres.NewMonitors(pool)
	ctx := t.Context()

	alice, err := users.Create(ctx, "alice@example.com", "hash")
	require.NoError(t, err)

	status := 204
	keyword := "ok"
	s := settings("api")
	s.Method = domain.MethodHead
	s.ExpectedStatus = &status
	s.Keyword = &keyword

	created, err := monitors.Create(ctx, alice.ID, s, 20)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, alice.ID, created.UserID)
	assert.Equal(t, s, created.MonitorSettings)
	assert.Equal(t, domain.StatusUnknown, created.Status)
	assert.Nil(t, created.LastCheckedAt)
	assert.WithinDuration(t, time.Now(), created.NextCheckAt, time.Minute, "a new monitor is due at once")

	got, err := monitors.Get(ctx, alice.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created, got)

	next := created.NextCheckAt.Add(time.Hour).Truncate(time.Microsecond)
	updated, err := monitors.Update(ctx, alice.ID, created.ID, func(m *domain.Monitor) error {
		m.Name = "renamed"
		m.ExpectedStatus = nil
		m.IsActive = false
		m.NextCheckAt = next
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)
	assert.Nil(t, updated.ExpectedStatus)
	assert.False(t, updated.IsActive)
	assert.True(t, next.Equal(updated.NextCheckAt))
	assert.True(t, updated.UpdatedAt.After(created.UpdatedAt) || updated.UpdatedAt.Equal(created.UpdatedAt))

	errAbort := errors.New("abort")
	_, err = monitors.Update(ctx, alice.ID, created.ID, func(m *domain.Monitor) error {
		m.Name = "never saved"
		return errAbort
	})
	require.ErrorIs(t, err, errAbort)
	got, err = monitors.Get(ctx, alice.ID, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", got.Name, "a failed update changes nothing")

	require.NoError(t, monitors.Delete(ctx, alice.ID, created.ID))
	_, err = monitors.Get(ctx, alice.ID, created.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, monitors.Delete(ctx, alice.ID, created.ID), domain.ErrNotFound)
}

// TestMonitorsIsolation: every method treats another user's monitor as
// missing.
func TestMonitorsIsolation(t *testing.T) {
	pool := pgtest.NewPool(t)
	users := postgres.NewUsers(pool)
	monitors := postgres.NewMonitors(pool)
	ctx := t.Context()

	alice, err := users.Create(ctx, "alice@example.com", "hash")
	require.NoError(t, err)
	mallory, err := users.Create(ctx, "mallory@example.com", "hash")
	require.NoError(t, err)
	m, err := monitors.Create(ctx, alice.ID, settings("alice-site"), 20)
	require.NoError(t, err)

	_, err = monitors.Get(ctx, mallory.ID, m.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	called := false
	_, err = monitors.Update(ctx, mallory.ID, m.ID, func(*domain.Monitor) error { called = true; return nil })
	require.ErrorIs(t, err, domain.ErrNotFound)
	assert.False(t, called)

	require.ErrorIs(t, monitors.Delete(ctx, mallory.ID, m.ID), domain.ErrNotFound)

	list, total, err := monitors.List(ctx, mallory.ID, 50, 0)
	require.NoError(t, err)
	assert.Empty(t, list)
	assert.Zero(t, total)

	_, err = monitors.Get(ctx, alice.ID, m.ID)
	require.NoError(t, err, "alice still has her monitor")
}

func TestMonitorsList(t *testing.T) {
	pool := pgtest.NewPool(t)
	users := postgres.NewUsers(pool)
	monitors := postgres.NewMonitors(pool)
	ctx := t.Context()

	alice, err := users.Create(ctx, "alice@example.com", "hash")
	require.NoError(t, err)
	for i := range 5 {
		_, err := monitors.Create(ctx, alice.ID, settings(fmt.Sprintf("m%d", i)), 20)
		require.NoError(t, err)
	}

	page := func(limit, offset int) []string {
		list, total, err := monitors.List(ctx, alice.ID, limit, offset)
		require.NoError(t, err)
		assert.Equal(t, 5, total)
		out := make([]string, 0, len(list))
		for _, m := range list {
			out = append(out, m.Name)
		}
		return out
	}

	assert.Equal(t, []string{"m4", "m3", "m2", "m1", "m0"}, page(50, 0), "newest first")
	assert.Equal(t, []string{"m4", "m3"}, page(2, 0))
	assert.Equal(t, []string{"m2", "m1"}, page(2, 2))
	assert.Equal(t, []string{"m0"}, page(2, 4))
	assert.Empty(t, page(2, 10))
}

// TestMonitorsLimitUnderConcurrency: many parallel creates of one user never
// exceed the limit (the user row lock serializes them).
func TestMonitorsLimitUnderConcurrency(t *testing.T) {
	pool := pgtest.NewPool(t)
	users := postgres.NewUsers(pool)
	monitors := postgres.NewMonitors(pool)
	ctx := t.Context()

	alice, err := users.Create(ctx, "alice@example.com", "hash")
	require.NoError(t, err)

	const limit, attempts = 3, 10
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		created  int
		rejected int
	)
	for i := range attempts {
		wg.Go(func() {
			_, err := monitors.Create(ctx, alice.ID, settings(fmt.Sprintf("m%d", i)), limit)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				created++
			case errors.Is(err, domain.ErrLimitExceeded):
				rejected++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
	wg.Wait()

	assert.Equal(t, limit, created)
	assert.Equal(t, attempts-limit, rejected)
	_, total, err := monitors.List(ctx, alice.ID, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, limit, total)
}
