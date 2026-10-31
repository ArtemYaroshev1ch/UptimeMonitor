//go:build integration

package postgres_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/postgres"
)

func failure(at time.Time, kind domain.ErrorKind) domain.CheckResult {
	return domain.CheckResult{CheckedAt: at, ResponseTimeMs: 100, ErrorKind: kind}
}

func success(at time.Time) domain.CheckResult {
	return domain.CheckResult{CheckedAt: at, Success: true, ResponseTimeMs: 100}
}

func (e checksEnv) openIncidents(t *testing.T, monitorID uuid.UUID) int {
	t.Helper()
	var n int
	err := e.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM incidents WHERE monitor_id = $1 AND resolved_at IS NULL`, monitorID).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestIncidentLifecycle walks one monitor (threshold 3) through an outage and
// a recovery, then a second outage.
func TestIncidentLifecycle(t *testing.T) {
	env := newChecksEnv(t)
	m := env.monitor(t, "api", true, time.Now())
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

	assert.Equal(t, domain.IncidentChange{}, env.save(t, m.ID, success(at(0))))
	assert.Equal(t, domain.IncidentChange{}, env.save(t, m.ID, failure(at(1), domain.ErrorConnection)))
	assert.Equal(t, domain.IncidentChange{}, env.save(t, m.ID, failure(at(2), domain.ErrorTimeout)), "2 of 3")

	opened := env.save(t, m.ID, failure(at(3), domain.ErrorTimeout)).Opened
	require.NotNil(t, opened, "the third failure in a row opens an incident")
	assert.Equal(t, m.ID, opened.MonitorID)
	assert.True(t, at(1).Equal(opened.StartedAt), "started at the first failure of the streak, not at the third")
	assert.Equal(t, domain.ErrorTimeout, opened.CauseKind)
	assert.True(t, opened.Open())

	assert.Equal(t, domain.IncidentChange{}, env.save(t, m.ID, failure(at(4), domain.ErrorTimeout)), "already open")
	assert.Equal(t, 1, env.openIncidents(t, m.ID))

	resolved := env.save(t, m.ID, success(at(5))).Resolved
	require.NotNil(t, resolved)
	assert.Equal(t, opened.ID, resolved.ID)
	require.NotNil(t, resolved.ResolvedAt)
	assert.True(t, at(5).Equal(*resolved.ResolvedAt))
	assert.Zero(t, env.openIncidents(t, m.ID))

	// A new outage opens a new incident; the resolved one does not block it.
	env.save(t, m.ID, failure(at(6), domain.ErrorDNS))
	env.save(t, m.ID, failure(at(7), domain.ErrorDNS))
	second := env.save(t, m.ID, failure(at(8), domain.ErrorDNS)).Opened
	require.NotNil(t, second)
	assert.NotEqual(t, opened.ID, second.ID)
	assert.True(t, at(6).Equal(second.StartedAt), "the streak starts after the last success")
}

// TestIncidentConcurrentResults is the guarantee of the phase: many results
// of the same monitor arriving at once (threshold 1, so every failure could
// open an incident) open exactly one incident, and many successes resolve it
// exactly once.
func TestIncidentConcurrentResults(t *testing.T) {
	env := newChecksEnv(t)
	s := settings("flappy")
	s.FailureThreshold = 1
	m, err := env.monitors.Create(t.Context(), env.user.ID, s, 10)
	require.NoError(t, err)

	const results = 20
	saveAll := func(res func(i int) domain.CheckResult) (opened, resolved int) {
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			start = make(chan struct{})
		)
		for i := range results {
			wg.Go(func() {
				<-start
				change, err := env.checks.SaveResult(t.Context(), m.ID, res(i))
				if !assert.NoError(t, err) {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if change.Opened != nil {
					opened++
				}
				if change.Resolved != nil {
					resolved++
				}
			})
		}
		close(start)
		wg.Wait()
		return opened, resolved
	}
	now := time.Now()

	opened, resolved := saveAll(func(i int) domain.CheckResult {
		return failure(now.Add(time.Duration(i)*time.Millisecond), domain.ErrorTimeout)
	})
	assert.Equal(t, 1, opened, "exactly one incident opened")
	assert.Zero(t, resolved)
	assert.Equal(t, 1, env.openIncidents(t, m.ID))

	got, err := env.monitors.Get(t.Context(), env.user.ID, m.ID)
	require.NoError(t, err)
	assert.Equal(t, results, got.ConsecutiveFailures, "every result was counted: none was lost")

	opened, resolved = saveAll(func(i int) domain.CheckResult {
		return success(now.Add(time.Second + time.Duration(i)*time.Millisecond))
	})
	assert.Zero(t, opened)
	assert.Equal(t, 1, resolved, "resolved exactly once")
	assert.Zero(t, env.openIncidents(t, m.ID))
}

// The partial unique index itself refuses a second open incident, whatever
// the application does.
func TestOneOpenIncidentIsEnforcedBySchema(t *testing.T) {
	env := newChecksEnv(t)
	m := env.monitor(t, "api", true, time.Now())
	insert := `INSERT INTO incidents (monitor_id) VALUES ($1)`

	_, err := env.pool.Exec(t.Context(), insert, m.ID)
	require.NoError(t, err)
	_, err = env.pool.Exec(t.Context(), insert, m.ID)

	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "want a PostgreSQL error, got %v", err)
	assert.Equal(t, "23505", pgErr.Code)
	assert.Equal(t, "uniq_open_incident_per_monitor", pgErr.ConstraintName)

	_, err = env.pool.Exec(t.Context(), `UPDATE incidents SET resolved_at = now() WHERE monitor_id = $1`, m.ID)
	require.NoError(t, err)
	_, err = env.pool.Exec(t.Context(), insert, m.ID)
	require.NoError(t, err, "resolved incidents do not count")
}

func TestIncidentsListAndGet(t *testing.T) {
	env := newChecksEnv(t)
	ctx := t.Context()
	incidents := postgres.NewIncidents(env.pool)
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// api: one resolved incident, then one open. web: one open.
	api := env.monitor(t, "api", true, time.Now())
	web := env.monitor(t, "web", true, time.Now())
	outage := func(m domain.Monitor, start time.Time, recovers bool) {
		for i := range 3 {
			env.save(t, m.ID, failure(start.Add(time.Duration(i)*time.Minute), domain.ErrorTimeout))
		}
		if recovers {
			env.save(t, m.ID, success(start.Add(5*time.Minute)))
		}
	}
	outage(api, t0, true)
	outage(api, t0.Add(time.Hour), false)
	outage(web, t0.Add(30*time.Minute), false)

	list := func(f domain.IncidentFilter) ([]domain.Incident, int) {
		items, total, err := incidents.List(ctx, env.user.ID, f, 50, 0)
		require.NoError(t, err)
		return items, total
	}

	all, total := list(domain.IncidentFilter{})
	assert.Equal(t, 3, total)
	require.Len(t, all, 3)
	assert.True(t, t0.Add(time.Hour).Equal(all[0].StartedAt), "newest first")
	assert.Equal(t, "api", all[0].MonitorName)
	assert.Equal(t, "web", all[1].MonitorName)

	_, total = list(domain.IncidentFilter{Open: new(true)})
	assert.Equal(t, 2, total)
	closed, total := list(domain.IncidentFilter{Open: new(false)})
	assert.Equal(t, 1, total)
	require.NotNil(t, closed[0].ResolvedAt)

	_, total = list(domain.IncidentFilter{MonitorID: &api.ID})
	assert.Equal(t, 2, total)
	_, total = list(domain.IncidentFilter{MonitorID: &api.ID, Open: new(true)})
	assert.Equal(t, 1, total)

	page, total, err := incidents.List(ctx, env.user.ID, domain.IncidentFilter{}, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page, 1)
	assert.Equal(t, all[1].ID, page[0].ID)

	got, err := incidents.Get(ctx, env.user.ID, all[0].ID)
	require.NoError(t, err)
	assert.Equal(t, all[0], got)

	// Another user sees nothing.
	other := uuid.New()
	items, total, err := incidents.List(ctx, other, domain.IncidentFilter{}, 50, 0)
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.Zero(t, total)
	_, err = incidents.Get(ctx, other, all[0].ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Deleting a monitor deletes its incidents.
	require.NoError(t, env.monitors.Delete(ctx, env.user.ID, api.ID))
	_, total = list(domain.IncidentFilter{})
	assert.Equal(t, 1, total)
}
