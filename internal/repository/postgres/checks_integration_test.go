//go:build integration

package postgres_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/postgres/pgtest"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/postgres"
)

type checksEnv struct {
	pool     *pgxpool.Pool
	monitors *postgres.Monitors
	checks   *postgres.Checks
	user     domain.User
}

func newChecksEnv(t *testing.T) checksEnv {
	t.Helper()
	pool := pgtest.NewPool(t)
	user, err := postgres.NewUsers(pool).Create(t.Context(), "alice@example.com", "hash")
	require.NoError(t, err)
	return checksEnv{pool: pool, monitors: postgres.NewMonitors(pool), checks: postgres.NewChecks(pool), user: user}
}

// monitor creates a monitor of the env's user and sets its next_check_at.
func (e checksEnv) monitor(t *testing.T, name string, active bool, nextCheck time.Time) domain.Monitor {
	t.Helper()
	s := settings(name)
	s.IsActive = active
	m, err := e.monitors.Create(t.Context(), e.user.ID, s, 1000)
	require.NoError(t, err)
	_, err = e.pool.Exec(t.Context(), `UPDATE monitors SET next_check_at = $2 WHERE id = $1`, m.ID, nextCheck)
	require.NoError(t, err)
	return m
}

func TestClaimDue(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	past := time.Now().Add(-time.Minute).Truncate(time.Microsecond)

	due := env.monitor(t, "due", true, past)
	env.monitor(t, "paused", false, past)
	env.monitor(t, "future", true, time.Now().Add(time.Hour))

	claimed, err := env.checks.ClaimDue(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	c := claimed[0]
	assert.Equal(t, due.ID, c.MonitorID)
	assert.Equal(t, due.URL, c.URL)
	assert.Equal(t, "GET", c.Method)
	assert.Equal(t, 10*time.Second, c.Timeout)
	assert.True(t, c.FollowRedirects)
	assert.True(t, past.Equal(c.ScheduledFor), "scheduled_for is next_check_at before the claim")

	stored, err := env.monitors.Get(ctx, env.user.ID, due.ID)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Minute), stored.NextCheckAt, 10*time.Second,
		"moved one interval (60 s) ahead")

	again, err := env.checks.ClaimDue(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, again, "a claimed monitor is not due any more")
}

// TestClaimDueConcurrently is the property that makes several workers safe:
// competing claimers never get the same monitor, and together they get all.
func TestClaimDueConcurrently(t *testing.T) {
	env := newChecksEnv(t)
	const monitors, workers, batch = 100, 4, 7
	past := time.Now().Add(-time.Minute)
	for i := range monitors {
		env.monitor(t, fmt.Sprintf("m%d", i), true, past)
	}

	var (
		mu      sync.Mutex
		claims  = map[uuid.UUID]int{}
		wg      sync.WaitGroup
		started = make(chan struct{})
	)
	for range workers {
		wg.Go(func() {
			<-started
			for {
				got, err := env.checks.ClaimDue(t.Context(), batch)
				if !assert.NoError(t, err) || len(got) == 0 {
					return
				}
				mu.Lock()
				for _, c := range got {
					claims[c.MonitorID]++
				}
				mu.Unlock()
			}
		})
	}
	close(started)
	wg.Wait()

	assert.Len(t, claims, monitors, "every due monitor was claimed")
	for id, n := range claims {
		assert.Equal(t, 1, n, "monitor %s claimed %d times", id, n)
	}
}

func TestSaveResult(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	m := env.monitor(t, "api", true, time.Now()) // failure_threshold 3
	now := time.Now().Truncate(time.Microsecond)
	notAfter := now.Add(30 * 24 * time.Hour)

	state := func() domain.Monitor {
		got, err := env.monitors.Get(ctx, env.user.ID, m.ID)
		require.NoError(t, err)
		return got
	}
	fail := domain.CheckResult{CheckedAt: now, ResponseTimeMs: 10000, ErrorKind: domain.ErrorTimeout, ErrorMessage: "timeout"}

	require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
		CheckedAt: now, Success: true, StatusCode: new(200), ResponseTimeMs: 120,
		DNSMs: new(5), ConnectMs: new(10), TLSMs: new(30), TTFBMs: new(70),
		HTTPProto: "HTTP/2.0", TLSVersion: "TLS1.3", TLSNotAfter: &notAfter,
	}))
	got := state()
	assert.Equal(t, domain.StatusUp, got.Status)
	assert.Zero(t, got.ConsecutiveFailures)
	require.NotNil(t, got.LastCheckedAt)
	assert.True(t, now.Equal(*got.LastCheckedAt))
	require.NotNil(t, got.TLSNotAfter)
	assert.True(t, notAfter.Equal(*got.TLSNotAfter))

	for i := 1; i <= 2; i++ {
		require.NoError(t, env.checks.SaveResult(ctx, m.ID, fail))
		got = state()
		assert.Equal(t, i, got.ConsecutiveFailures)
		assert.Equal(t, domain.StatusUp, got.Status, "below the threshold the status stays")
	}
	require.NotNil(t, got.TLSNotAfter, "a failed check keeps the last known certificate expiry")

	require.NoError(t, env.checks.SaveResult(ctx, m.ID, fail))
	assert.Equal(t, domain.StatusDown, state().Status, "the third failure in a row reaches the threshold")

	require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{CheckedAt: now, Success: true, ResponseTimeMs: 50}))
	got = state()
	assert.Equal(t, domain.StatusUp, got.Status)
	assert.Zero(t, got.ConsecutiveFailures)

	require.NoError(t, env.monitors.Delete(ctx, env.user.ID, m.ID))
	require.ErrorIs(t, env.checks.SaveResult(ctx, m.ID, fail), domain.ErrNotFound)
}

func TestListChecks(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	m := env.monitor(t, "api", true, time.Now())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
			CheckedAt: base.Add(time.Duration(i) * time.Minute), Success: i != 2, ResponseTimeMs: 100 + i,
			ErrorKind: map[bool]domain.ErrorKind{true: domain.ErrorDNS}[i == 2],
		}))
	}

	list, total, err := env.checks.List(ctx, env.user.ID, m.ID, base.Add(time.Minute), base.Add(4*time.Minute), 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total, "minutes 1, 2 and 3: from is inclusive, to exclusive")
	require.Len(t, list, 2)
	assert.Equal(t, 103, list[0].ResponseTimeMs, "newest first")
	assert.Equal(t, 102, list[1].ResponseTimeMs)
	assert.Equal(t, domain.ErrorDNS, list[1].ErrorKind)
	assert.False(t, list[1].Success)
	assert.Empty(t, list[0].HTTPProto, "NULL becomes empty")

	list, _, err = env.checks.List(ctx, env.user.ID, m.ID, base.Add(time.Minute), base.Add(4*time.Minute), 2, 2)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 101, list[0].ResponseTimeMs)

	list, total, err = env.checks.List(ctx, uuid.New(), m.ID, base, base.Add(time.Hour), 50, 0)
	require.NoError(t, err)
	assert.Empty(t, list, "another user sees nothing")
	assert.Zero(t, total)
}

func TestStats(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	m := env.monitor(t, "api", true, time.Now())
	now := time.Now()

	empty, err := env.checks.Stats(ctx, env.user.ID, m.ID, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, domain.MonitorStats{}, empty, "no checks: zero counts, nil figures")

	for i, ms := range []int{100, 200, 300} {
		require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
			CheckedAt: now.Add(-time.Duration(i+1) * time.Minute), Success: true,
			ResponseTimeMs: ms, TTFBMs: new(ms / 10),
		}))
	}
	require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
		CheckedAt: now.Add(-5 * time.Minute), ResponseTimeMs: 10000, ErrorKind: domain.ErrorTimeout,
	}))
	// Outside the period: must not count.
	require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
		CheckedAt: now.Add(-2 * time.Hour), ResponseTimeMs: 1, ErrorKind: domain.ErrorDNS,
	}))

	got, err := env.checks.Stats(ctx, env.user.ID, m.ID, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, domain.MonitorStats{
		TotalChecks:   4,
		FailedChecks:  1,
		UptimePercent: new(75.0),
		AvgResponseMs: new(200.0), // successful checks only
		// percentile_cont interpolates: 200 + 0.9 * (300 - 200).
		P95ResponseMs: new(290.0),
		P95TTFBMs:     new(29.0),
	}, got)

	other, err := env.checks.Stats(ctx, uuid.New(), m.ID, now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, other.TotalChecks, "another user's monitor has no stats")
}

func TestDeleteBefore(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	m := env.monitor(t, "api", true, time.Now())
	cutoff := time.Now().Add(-24 * time.Hour)
	for i := range 7 {
		age := 48 * time.Hour // old
		if i >= 5 {
			age = time.Hour // recent
		}
		require.NoError(t, env.checks.SaveResult(ctx, m.ID, domain.CheckResult{
			CheckedAt: time.Now().Add(-age), Success: true, ResponseTimeMs: 1,
		}))
	}

	deleted, err := env.checks.DeleteBefore(ctx, cutoff, 2) // 3 batches: 2 + 2 + 1
	require.NoError(t, err)
	assert.Equal(t, int64(5), deleted)

	_, total, err := env.checks.List(ctx, env.user.ID, m.ID, time.Now().Add(-72*time.Hour), time.Now(), 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total, "recent checks are kept")
}
