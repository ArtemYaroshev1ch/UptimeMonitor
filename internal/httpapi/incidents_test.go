package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// seedIncidents gives a monitor of token's user one resolved and one open
// incident and returns their IDs.
func seedIncidents(t *testing.T, api testAPI, monitorID string) (resolved, open uuid.UUID) {
	t.Helper()
	id := uuid.MustParse(monitorID)
	now := api.clock.Now()
	resolvedAt := now.Add(-time.Hour)
	r := api.incidents.Add(domain.Incident{
		MonitorID: id, StartedAt: now.Add(-time.Hour - 90*time.Second), ResolvedAt: &resolvedAt, CauseKind: domain.ErrorDNS,
	})
	o := api.incidents.Add(domain.Incident{MonitorID: id, StartedAt: now.Add(-time.Minute), CauseKind: domain.ErrorTimeout})
	return r.ID, o.ID
}

func TestIncidentsEndpoints(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	m := api.createMonitor(t, token, `{"name":"api","url":"https://example.com"}`)
	other := api.createMonitor(t, token, `{"name":"web","url":"https://example.org"}`)
	resolvedID, openID := seedIncidents(t, api, m.ID)
	api.incidents.Add(domain.Incident{MonitorID: uuid.MustParse(other.ID), StartedAt: api.clock.Now().Add(-3 * time.Hour)})

	rec := api.do(t, http.MethodGet, "/api/v1/incidents/"+resolvedID.String(), "", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"id": "`+resolvedID.String()+`", "monitor_id": "`+m.ID+`", "monitor_name": "api",
		"started_at": "2026-01-02T02:02:35Z", "resolved_at": "2026-01-02T02:04:05Z",
		"cause_kind": "dns", "duration_seconds": 90
	}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/incidents/"+openID.String(), "", token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"resolved_at":null`)
	assert.Contains(t, rec.Body.String(), `"duration_seconds":null`)

	ids := func(query string) ([]string, int) {
		rec := api.do(t, http.MethodGet, "/api/v1/incidents"+query, "", token)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		page := decode[struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			Total int `json:"total"`
		}](t, rec.Body.Bytes())
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out, page.Total
	}

	_, total := ids("")
	assert.Equal(t, 3, total)
	got, total := ids("?monitor_id=" + m.ID)
	assert.Equal(t, 2, total)
	assert.Equal(t, []string{openID.String(), resolvedID.String()}, got, "newest first")
	got, _ = ids("?monitor_id=" + m.ID + "&open=true")
	assert.Equal(t, []string{openID.String()}, got)
	got, _ = ids("?open=false")
	assert.Equal(t, []string{resolvedID.String()}, got)
	got, total = ids("?limit=1&offset=1")
	assert.Equal(t, 3, total)
	assert.Len(t, got, 1)
}

func TestIncidentsQueryErrors(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")

	rec := api.do(t, http.MethodGet, "/api/v1/incidents?monitor_id=nope&open=yes&limit=0", "", token)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"limit","issue":"must be between 1 and 200"},
		{"field":"monitor_id","issue":"must be a UUID"},
		{"field":"open","issue":"must be true or false"}]}}`, rec.Body.String())
}

func TestIncidentsIsolation(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	alice := api.userToken(t, "alice@example.com")
	mallory := api.userToken(t, "mallory@example.com")
	m := api.createMonitor(t, alice, `{"name":"api","url":"https://example.com"}`)
	resolvedID, _ := seedIncidents(t, api, m.ID)

	rec := api.do(t, http.MethodGet, "/api/v1/incidents/"+resolvedID.String(), "", mallory)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = api.do(t, http.MethodGet, "/api/v1/incidents", "", mallory)
	assert.JSONEq(t, `{"items":[],"total":0}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/incidents?monitor_id="+m.ID, "", mallory)
	assert.JSONEq(t, `{"items":[],"total":0}`, rec.Body.String(), "filtering by a foreign monitor shows nothing")

	for _, path := range []string{"/api/v1/incidents/not-a-uuid", "/api/v1/incidents/" + uuid.NewString()} {
		rec = api.do(t, http.MethodGet, path, "", alice)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}

	rec = api.do(t, http.MethodGet, "/api/v1/incidents", "", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
