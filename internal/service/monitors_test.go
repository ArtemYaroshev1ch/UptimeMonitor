package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/checker"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/memory"
)

type monitorsEnv struct {
	svc   *Monitors
	repo  *memory.Monitors
	clock *clock.Fake
	user  uuid.UUID
}

func newMonitorsEnv(t *testing.T) monitorsEnv {
	t.Helper()
	clk := clock.NewFake(testNow)
	repo := memory.NewMonitors(clk.Now)
	return monitorsEnv{
		svc:   NewMonitors(repo, checker.NewTargetPolicy(false), clk, 3),
		repo:  repo,
		clock: clk,
		user:  uuid.New(),
	}
}

// patch decodes JSON into a MonitorPatch, the way the HTTP handler does.
func patch(t *testing.T, body string) MonitorPatch {
	t.Helper()
	var p MonitorPatch
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	return p
}

func (e monitorsEnv) create(t *testing.T, body string) domain.Monitor {
	t.Helper()
	m, err := e.svc.Create(t.Context(), e.user, patch(t, body))
	require.NoError(t, err)
	return m
}

func ptr[T any](v T) *T { return &v }

func TestOptionalUnmarshal(t *testing.T) {
	t.Parallel()

	var p MonitorPatch
	require.NoError(t, json.Unmarshal([]byte(`{"name":"api","keyword":null}`), &p))

	assert.Equal(t, Optional[string]{Set: true, Value: "api"}, p.Name)
	assert.Equal(t, Optional[string]{Set: true, Null: true}, p.Keyword)
	assert.Equal(t, Optional[string]{}, p.URL, "absent")
	assert.Equal(t, Some("x"), Optional[string]{Set: true, Value: "x"})
}

func TestCreateMonitorDefaults(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)

	m := env.create(t, `{"name":"  API  ","url":" https://example.com/health "}`)

	want := domain.DefaultMonitorSettings()
	want.Name = "API"
	want.URL = "https://example.com/health"
	assert.Equal(t, want, m.MonitorSettings)
	assert.Equal(t, env.user, m.UserID)
}

func TestCreateMonitorAllFields(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)

	m := env.create(t, `{
		"name": "API", "url": "http://example.com", "method": "head",
		"interval_seconds": 300, "timeout_seconds": 5, "expected_status": 204,
		"keyword": "ok", "follow_redirects": false, "failure_threshold": 1,
		"cert_expiry_warn_days": 30, "is_active": false}`)

	assert.Equal(t, domain.MonitorSettings{
		Name: "API", URL: "http://example.com", Method: "HEAD",
		IntervalSeconds: 300, TimeoutSeconds: 5, ExpectedStatus: ptr(204),
		Keyword: ptr("ok"), FollowRedirects: false, FailureThreshold: 1,
		CertExpiryWarnDays: 30, IsActive: false,
	}, m.MonitorSettings)
}

func TestCreateMonitorValidation(t *testing.T) {
	t.Parallel()
	const valid = `"name":"api","url":"https://example.com"`

	tests := []struct {
		name string
		body string
		want []domain.FieldError
	}{
		{
			name: "empty",
			body: `{}`,
			want: []domain.FieldError{{Field: "name", Issue: "is required"}, {Field: "url", Issue: "is required"}},
		},
		{
			name: "blank name",
			body: `{"name":"   ","url":"https://example.com"}`,
			want: []domain.FieldError{{Field: "name", Issue: "is required"}},
		},
		{
			name: "long name",
			body: `{"name":"` + strings.Repeat("я", 101) + `","url":"https://example.com"}`,
			want: []domain.FieldError{{Field: "name", Issue: "must be at most 100 characters"}},
		},
		{
			name: "null for a required field",
			body: `{"name":null,"url":"https://example.com"}`,
			want: []domain.FieldError{{Field: "name", Issue: "must not be null"}},
		},
		{
			name: "blocked URL",
			body: `{"name":"api","url":"http://169.254.169.254/latest/meta-data"}`,
			want: []domain.FieldError{{Field: "url", Issue: "must point to a public address"}},
		},
		{
			name: "bad scheme",
			body: `{"name":"api","url":"ftp://example.com"}`,
			want: []domain.FieldError{{Field: "url", Issue: "must use http or https"}},
		},
		{
			name: "method",
			body: `{` + valid + `,"method":"POST"}`,
			want: []domain.FieldError{{Field: "method", Issue: "must be one of: GET, HEAD"}},
		},
		{
			name: "interval too short",
			body: `{` + valid + `,"interval_seconds":29}`,
			want: []domain.FieldError{{Field: "interval_seconds", Issue: "must be at least 30"}},
		},
		{
			name: "interval too long",
			body: `{` + valid + `,"interval_seconds":86401}`,
			want: []domain.FieldError{{Field: "interval_seconds", Issue: "must be at most 86400"}},
		},
		{
			name: "timeout",
			body: `{` + valid + `,"timeout_seconds":31}`,
			want: []domain.FieldError{{Field: "timeout_seconds", Issue: "must be at most 30"}},
		},
		{
			name: "expected status",
			body: `{` + valid + `,"expected_status":99}`,
			want: []domain.FieldError{{Field: "expected_status", Issue: "must be at least 100"}},
		},
		{
			name: "empty keyword",
			body: `{` + valid + `,"keyword":""}`,
			want: []domain.FieldError{{Field: "keyword", Issue: "must be at least 1 characters"}},
		},
		{
			name: "threshold",
			body: `{` + valid + `,"failure_threshold":11}`,
			want: []domain.FieldError{{Field: "failure_threshold", Issue: "must be at most 10"}},
		},
		{
			name: "cert days",
			body: `{` + valid + `,"cert_expiry_warn_days":0}`,
			want: []domain.FieldError{{Field: "cert_expiry_warn_days", Issue: "must be at least 1"}},
		},
		{
			name: "several problems, sorted by field",
			body: `{"url":"http://localhost","timeout_seconds":0,"is_active":null}`,
			want: []domain.FieldError{
				{Field: "is_active", Issue: "must not be null"},
				{Field: "name", Issue: "is required"},
				{Field: "timeout_seconds", Issue: "must be at least 1"},
				{Field: "url", Issue: "must point to a public address"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newMonitorsEnv(t)

			_, err := env.svc.Create(t.Context(), env.user, patch(t, tt.body))

			assert.Equal(t, tt.want, validationDetails(t, err))
			assert.Zero(t, env.repo.Len())
		})
	}
}

func TestCreateMonitorNullableFields(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)

	m := env.create(t, `{"name":"api","url":"https://example.com","expected_status":null,"keyword":null}`)

	assert.Nil(t, m.ExpectedStatus)
	assert.Nil(t, m.Keyword)
}

func TestCreateMonitorLimit(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t) // limit 3
	for range 3 {
		env.create(t, `{"name":"api","url":"https://example.com"}`)
	}

	_, err := env.svc.Create(t.Context(), env.user, patch(t, `{"name":"api","url":"https://example.com"}`))
	require.ErrorIs(t, err, domain.ErrLimitExceeded)

	_, err = env.svc.Create(t.Context(), uuid.New(), patch(t, `{"name":"api","url":"https://example.com"}`))
	require.NoError(t, err, "the limit is per user")
}

func TestUpdateMonitor(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	m := env.create(t, `{"name":"api","url":"https://example.com","keyword":"ok","expected_status":200}`)

	got, err := env.svc.Update(t.Context(), env.user, m.ID, patch(t, `{"name":"renamed","keyword":null}`))

	require.NoError(t, err)
	want := m.MonitorSettings
	want.Name = "renamed"
	want.Keyword = nil
	assert.Equal(t, want, got.MonitorSettings, "only the given fields change")
}

func TestUpdateMonitorValidation(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	m := env.create(t, `{"name":"api","url":"https://example.com"}`)

	_, err := env.svc.Update(t.Context(), env.user, m.ID,
		patch(t, `{"name":"renamed","url":"http://10.0.0.1","interval_seconds":null}`))

	assert.Equal(t, []domain.FieldError{
		{Field: "interval_seconds", Issue: "must not be null"},
		{Field: "url", Issue: "must point to a public address"},
	}, validationDetails(t, err))
	stored, err := env.repo.Get(t.Context(), env.user, m.ID)
	require.NoError(t, err)
	assert.Equal(t, m, stored, "nothing is saved")
}

// A monitor whose URL the current policy would reject (created while
// ALLOW_PRIVATE_TARGETS was on) can still be edited without touching the URL.
func TestUpdateMonitorKeepsGrandfatheredURL(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	s := domain.DefaultMonitorSettings()
	s.Name, s.URL = "local", "http://localhost:8080"
	m, err := env.repo.Create(t.Context(), env.user, s, 10)
	require.NoError(t, err)

	_, err = env.svc.Update(t.Context(), env.user, m.ID, patch(t, `{"is_active":false}`))
	require.NoError(t, err)

	_, err = env.svc.Update(t.Context(), env.user, m.ID, patch(t, `{"url":"http://localhost:8080"}`))
	assert.Equal(t, []domain.FieldError{{Field: "url", Issue: "must point to a public address"}}, validationDetails(t, err))
}

func TestUpdateMonitorReschedules(t *testing.T) {
	t.Parallel()

	later := testNow.Add(10 * time.Hour)
	tests := []struct {
		name     string
		initial  string
		update   string
		wantNext time.Time
	}{
		{
			name:     "unrelated change keeps the schedule",
			initial:  `{"interval_seconds":86400}`,
			update:   `{"name":"renamed"}`,
			wantNext: later,
		},
		{
			name:     "resume checks at once",
			initial:  `{"is_active":false}`,
			update:   `{"is_active":true}`,
			wantNext: testNow,
		},
		{
			name:     "new URL checks at once",
			initial:  `{}`,
			update:   `{"url":"https://example.org"}`,
			wantNext: testNow,
		},
		{
			name:     "shorter interval pulls the next check in",
			initial:  `{"interval_seconds":86400}`,
			update:   `{"interval_seconds":60}`,
			wantNext: testNow.Add(time.Minute),
		},
		{
			name:     "paused monitor is not rescheduled",
			initial:  `{"is_active":false}`,
			update:   `{"url":"https://example.org"}`,
			wantNext: later,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newMonitorsEnv(t)
			initial := strings.TrimSuffix(tt.initial, "}")
			if initial != "{" {
				initial += ","
			}
			m := env.create(t, initial+`"name":"api","url":"https://example.com"}`)
			stored := m
			stored.NextCheckAt = later
			env.repo.Put(stored)

			got, err := env.svc.Update(t.Context(), env.user, m.ID, patch(t, tt.update))

			require.NoError(t, err)
			assert.Equal(t, tt.wantNext, got.NextCheckAt)
		})
	}
}

func TestMonitorsOfOtherUsers(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	m := env.create(t, `{"name":"api","url":"https://example.com"}`)
	other := uuid.New()

	_, err := env.svc.Get(t.Context(), other, m.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = env.svc.Update(t.Context(), other, m.ID, patch(t, `{"name":"x"}`))
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.ErrorIs(t, env.svc.Delete(t.Context(), other, m.ID), domain.ErrNotFound)
	page, err := env.svc.List(t.Context(), other, 50, 0)
	require.NoError(t, err)
	assert.Zero(t, page.Total)
}

func TestMonitorsGetListDelete(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	m := env.create(t, `{"name":"api","url":"https://example.com"}`)

	got, err := env.svc.Get(t.Context(), env.user, m.ID)
	require.NoError(t, err)
	assert.Equal(t, m, got)

	page, err := env.svc.List(t.Context(), env.user, 50, 0)
	require.NoError(t, err)
	assert.Equal(t, MonitorPage{Items: []domain.Monitor{m}, Total: 1}, page)

	require.NoError(t, env.svc.Delete(t.Context(), env.user, m.ID))
	_, err = env.svc.Get(t.Context(), env.user, m.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestMonitorsRepositoryFailure(t *testing.T) {
	t.Parallel()
	env := newMonitorsEnv(t)
	m := env.create(t, `{"name":"api","url":"https://example.com"}`)
	dbErr := errors.New("connection reset")
	env.repo.FailWith(dbErr)
	ctx := t.Context()

	_, err := env.svc.Create(ctx, env.user, patch(t, `{"name":"api","url":"https://example.com"}`))
	require.ErrorIs(t, err, dbErr)
	_, err = env.svc.Get(ctx, env.user, m.ID)
	require.ErrorIs(t, err, dbErr)
	_, err = env.svc.List(ctx, env.user, 50, 0)
	require.ErrorIs(t, err, dbErr)
	_, err = env.svc.Update(ctx, env.user, m.ID, patch(t, `{}`))
	require.ErrorIs(t, err, dbErr)
	require.ErrorIs(t, env.svc.Delete(ctx, env.user, m.ID), dbErr)
}
