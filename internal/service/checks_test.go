package service

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/memory"
)

type checksEnv struct {
	svc      *Checks
	monitors *memory.Monitors
	checks   *memory.Checks
	user     uuid.UUID
	monitor  domain.Monitor
}

func newChecksEnv(t *testing.T) checksEnv {
	t.Helper()
	clk := clock.NewFake(testNow)
	monitors := memory.NewMonitors(clk.Now)
	checks := memory.NewChecks(monitors)
	user := uuid.New()
	s := domain.DefaultMonitorSettings()
	s.Name, s.URL = "api", "https://example.com"
	m, err := monitors.Create(t.Context(), user, s, 10)
	require.NoError(t, err)
	return checksEnv{svc: NewChecks(monitors, checks, clk), monitors: monitors, checks: checks, user: user, monitor: m}
}

func TestListChecksDefaultsToLastDay(t *testing.T) {
	t.Parallel()
	env := newChecksEnv(t)
	env.checks.Add(env.monitor.ID, domain.CheckResult{CheckedAt: testNow.Add(-time.Hour), Success: true})
	env.checks.Add(env.monitor.ID, domain.CheckResult{CheckedAt: testNow.Add(-25 * time.Hour), Success: true})

	page, err := env.svc.List(t.Context(), env.user, env.monitor.ID, CheckQuery{Limit: 50})

	require.NoError(t, err)
	assert.Equal(t, 1, page.Total, "only the last 24 hours")
	assert.Equal(t, testNow, page.To)
	assert.Equal(t, testNow.Add(-24*time.Hour), page.From)
}

func TestListChecksRange(t *testing.T) {
	t.Parallel()
	env := newChecksEnv(t)
	for i := range 5 {
		env.checks.Add(env.monitor.ID, domain.CheckResult{CheckedAt: testNow.Add(-time.Duration(i) * time.Hour), ResponseTimeMs: i})
	}
	from, to := testNow.Add(-3*time.Hour), testNow.Add(-time.Hour)

	page, err := env.svc.List(t.Context(), env.user, env.monitor.ID, CheckQuery{From: &from, To: &to, Limit: 50})

	require.NoError(t, err)
	require.Equal(t, 2, page.Total, "-3h and -2h: from inclusive, to exclusive")
	assert.Equal(t, 2, page.Items[0].ResponseTimeMs)
	assert.Equal(t, 3, page.Items[1].ResponseTimeMs)
}

func TestListChecksErrors(t *testing.T) {
	t.Parallel()
	env := newChecksEnv(t)
	from := testNow
	to := testNow.Add(-time.Hour)

	_, err := env.svc.List(t.Context(), env.user, env.monitor.ID, CheckQuery{From: &from, To: &to, Limit: 50})
	assert.Equal(t, []domain.FieldError{{Field: "from", Issue: "must be before to"}}, validationDetails(t, err))

	_, err = env.svc.List(t.Context(), uuid.New(), env.monitor.ID, CheckQuery{Limit: 50})
	require.ErrorIs(t, err, domain.ErrNotFound, "another user's monitor")

	dbErr := errors.New("connection reset")
	env.checks.FailWith(dbErr)
	_, err = env.svc.List(t.Context(), env.user, env.monitor.ID, CheckQuery{Limit: 50})
	require.ErrorIs(t, err, dbErr)
}

func TestStats(t *testing.T) {
	t.Parallel()
	env := newChecksEnv(t)
	for i, ms := range []int{100, 200, 300} {
		env.checks.Add(env.monitor.ID, domain.CheckResult{
			CheckedAt: testNow.Add(-time.Duration(i+1) * time.Hour), Success: true, ResponseTimeMs: ms, TTFBMs: new(ms / 10),
		})
	}
	env.checks.Add(env.monitor.ID, domain.CheckResult{CheckedAt: testNow.Add(-5 * time.Hour), ResponseTimeMs: 10000})
	env.checks.Add(env.monitor.ID, domain.CheckResult{CheckedAt: testNow.Add(-3 * 24 * time.Hour), Success: true, ResponseTimeMs: 1})

	day, err := env.svc.Stats(t.Context(), env.user, env.monitor.ID, "")
	require.NoError(t, err)
	assert.Equal(t, "24h", day.Period)
	assert.Equal(t, testNow.Add(-24*time.Hour), day.From)
	assert.Equal(t, domain.MonitorStats{
		TotalChecks: 4, FailedChecks: 1, UptimePercent: new(75.0),
		AvgResponseMs: new(200.0), P95ResponseMs: new(290.0), P95TTFBMs: new(29.0),
	}, day.MonitorStats)

	week, err := env.svc.Stats(t.Context(), env.user, env.monitor.ID, "7d")
	require.NoError(t, err)
	assert.Equal(t, 5, week.TotalChecks)
}

func TestStatsErrors(t *testing.T) {
	t.Parallel()
	env := newChecksEnv(t)

	_, err := env.svc.Stats(t.Context(), env.user, env.monitor.ID, "1y")
	assert.Equal(t, []domain.FieldError{{Field: "period", Issue: "must be one of: 24h, 7d, 30d"}}, validationDetails(t, err))

	_, err = env.svc.Stats(t.Context(), uuid.New(), env.monitor.ID, "24h")
	require.ErrorIs(t, err, domain.ErrNotFound)

	dbErr := errors.New("connection reset")
	env.checks.FailWith(dbErr)
	_, err = env.svc.Stats(t.Context(), env.user, env.monitor.ID, "30d")
	require.ErrorIs(t, err, dbErr)
}
