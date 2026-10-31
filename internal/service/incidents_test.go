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

func TestIncidents(t *testing.T) {
	t.Parallel()
	monitors := memory.NewMonitors(clock.NewFake(testNow).Now)
	repo := memory.NewIncidents(monitors)
	svc := NewIncidents(repo)
	user := uuid.New()
	s := domain.DefaultMonitorSettings()
	s.Name, s.URL = "api", "https://example.com"
	m, err := monitors.Create(t.Context(), user, s, 10)
	require.NoError(t, err)
	resolvedAt := testNow.Add(-time.Hour)
	closed := repo.Add(domain.Incident{MonitorID: m.ID, StartedAt: testNow.Add(-2 * time.Hour), ResolvedAt: &resolvedAt})
	open := repo.Add(domain.Incident{MonitorID: m.ID, StartedAt: testNow.Add(-time.Minute), CauseKind: domain.ErrorTimeout})

	page, err := svc.List(t.Context(), user, domain.IncidentFilter{}, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, page.Total)
	assert.Equal(t, open.ID, page.Items[0].ID, "newest first")
	assert.Equal(t, "api", page.Items[0].MonitorName)

	page, err = svc.List(t.Context(), user, domain.IncidentFilter{Open: new(false)}, 50, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, closed.ID, page.Items[0].ID)

	got, err := svc.Get(t.Context(), user, open.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.ErrorTimeout, got.CauseKind)

	_, err = svc.Get(t.Context(), uuid.New(), open.ID)
	require.ErrorIs(t, err, domain.ErrNotFound, "another user's incident")

	dbErr := errors.New("connection reset")
	repo.FailWith(dbErr)
	_, err = svc.List(t.Context(), user, domain.IncidentFilter{}, 50, 0)
	require.ErrorIs(t, err, dbErr)
	_, err = svc.Get(t.Context(), user, open.ID)
	require.ErrorIs(t, err, dbErr)
}
