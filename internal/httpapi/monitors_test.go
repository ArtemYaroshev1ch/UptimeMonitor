package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type monitorJSON struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	URL                string  `json:"url"`
	Method             string  `json:"method"`
	IntervalSeconds    int     `json:"interval_seconds"`
	TimeoutSeconds     int     `json:"timeout_seconds"`
	ExpectedStatus     *int    `json:"expected_status"`
	Keyword            *string `json:"keyword"`
	FollowRedirects    bool    `json:"follow_redirects"`
	FailureThreshold   int     `json:"failure_threshold"`
	CertExpiryWarnDays int     `json:"cert_expiry_warn_days"`
	IsActive           bool    `json:"is_active"`
	Status             string  `json:"status"`
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(body, &v), string(body))
	return v
}

func (a testAPI) createMonitor(t *testing.T, token, body string) monitorJSON {
	t.Helper()
	rec := a.do(t, http.MethodPost, "/api/v1/monitors", body, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return decode[monitorJSON](t, rec.Body.Bytes())
}

func TestMonitorsCRUD(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")

	// Create with defaults.
	rec := api.do(t, http.MethodPost, "/api/v1/monitors", `{"name":"Example","url":"https://example.com"}`, token)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decode[monitorJSON](t, rec.Body.Bytes())
	assert.Equal(t, "/api/v1/monitors/"+created.ID, rec.Header().Get("Location"))
	assert.JSONEq(t, `{
		"id": "`+created.ID+`", "name": "Example", "url": "https://example.com",
		"method": "GET", "interval_seconds": 60, "timeout_seconds": 10,
		"expected_status": null, "keyword": null, "follow_redirects": true,
		"failure_threshold": 3, "cert_expiry_warn_days": 14, "is_active": true,
		"status": "unknown", "tls_not_after": null, "last_checked_at": null,
		"created_at": "2026-01-02T03:04:05Z", "updated_at": "2026-01-02T03:04:05Z"
	}`, rec.Body.String())

	// Read.
	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, created, decode[monitorJSON](t, rec.Body.Bytes()))

	// Partial update: pause and set a keyword; everything else stays.
	rec = api.do(t, http.MethodPatch, "/api/v1/monitors/"+created.ID, `{"is_active":false,"keyword":"Example Domain"}`, token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	updated := decode[monitorJSON](t, rec.Body.Bytes())
	want := created
	want.IsActive = false
	want.Keyword = new("Example Domain")
	assert.Equal(t, want, updated)

	// Clear the keyword with an explicit null.
	rec = api.do(t, http.MethodPatch, "/api/v1/monitors/"+created.ID, `{"keyword":null}`, token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, decode[monitorJSON](t, rec.Body.Bytes()).Keyword)

	// List.
	rec = api.do(t, http.MethodGet, "/api/v1/monitors", "", token)
	require.Equal(t, http.StatusOK, rec.Code)
	list := decode[struct {
		Items []monitorJSON `json:"items"`
		Total int           `json:"total"`
	}](t, rec.Body.Bytes())
	assert.Equal(t, 1, list.Total)
	require.Len(t, list.Items, 1)
	assert.Equal(t, created.ID, list.Items[0].ID)

	// Delete.
	rec = api.do(t, http.MethodDelete, "/api/v1/monitors/"+created.ID, "", token)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+created.ID, "", token)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMonitorsValidationDetails(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")

	rec := api.do(t, http.MethodPost, "/api/v1/monitors",
		`{"name":"","url":"http://127.0.0.1:9090/metrics","method":"POST","interval_seconds":10}`, token)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"interval_seconds","issue":"must be at least 30"},
		{"field":"method","issue":"must be one of: GET, HEAD"},
		{"field":"name","issue":"is required"},
		{"field":"url","issue":"must point to a public address"}
	]}}`, rec.Body.String())
	assert.Zero(t, api.monitors.Len())
}

func TestMonitorsWrongJSONType(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")

	rec := api.do(t, http.MethodPost, "/api/v1/monitors", `{"name":"x","url":"https://example.com","interval_seconds":"60"}`, token)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"interval_seconds","issue":"must be an integer"}]}}`, rec.Body.String())
}

func TestMonitorsLimit(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	for i := range testMonitorLimit {
		api.createMonitor(t, token, fmt.Sprintf(`{"name":"m%d","url":"https://example.com"}`, i))
	}

	rec := api.do(t, http.MethodPost, "/api/v1/monitors", `{"name":"one more","url":"https://example.com"}`, token)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"conflict","message":"Limit reached"}}`, rec.Body.String())
}

func TestMonitorsPagination(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	for i := range testMonitorLimit {
		api.createMonitor(t, token, fmt.Sprintf(`{"name":"m%d","url":"https://example.com"}`, i))
	}

	names := func(query string) ([]string, int) {
		rec := api.do(t, http.MethodGet, "/api/v1/monitors"+query, "", token)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		page := decode[struct {
			Items []monitorJSON `json:"items"`
			Total int           `json:"total"`
		}](t, rec.Body.Bytes())
		out := []string{}
		for _, m := range page.Items {
			out = append(out, m.Name)
		}
		return out, page.Total
	}

	got, total := names("")
	assert.Equal(t, []string{"m2", "m1", "m0"}, got, "newest first")
	assert.Equal(t, 3, total)
	got, total = names("?limit=2&offset=1")
	assert.Equal(t, []string{"m1", "m0"}, got)
	assert.Equal(t, 3, total)
	got, total = names("?offset=10")
	assert.Empty(t, got, "an empty page is [] not null")
	assert.Equal(t, 3, total)

	rec := api.do(t, http.MethodGet, "/api/v1/monitors?limit=500&offset=-1", "", token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"limit","issue":"must be between 1 and 200"},
		{"field":"offset","issue":"must not be negative"}]}}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/monitors?limit=ten&offset=x", "", token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"limit","issue":"must be an integer"},
		{"field":"offset","issue":"must be an integer"}]}}`, rec.Body.String())
}

// TestMonitorsIsolation: another user's monitor behaves exactly like a
// missing one for every endpoint (404, nothing changed), and lists contain
// only the caller's monitors.
func TestMonitorsIsolation(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	alice := api.userToken(t, "alice@example.com")
	mallory := api.userToken(t, "mallory@example.com")
	m := api.createMonitor(t, alice, `{"name":"alice's","url":"https://example.com"}`)
	path := "/api/v1/monitors/" + m.ID
	notFound := `{"error":{"code":"not_found","message":"Resource not found"}}`

	tests := []struct {
		name   string
		method string
		body   string
	}{
		{name: "get", method: http.MethodGet},
		{name: "update", method: http.MethodPatch, body: `{"name":"pwned"}`},
		{name: "delete", method: http.MethodDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := api.do(t, tt.method, path, tt.body, mallory)

			assert.Equal(t, http.StatusNotFound, rec.Code)
			assert.JSONEq(t, notFound, rec.Body.String())
		})
	}

	rec := api.do(t, http.MethodGet, "/api/v1/monitors", "", mallory)
	assert.JSONEq(t, `{"items":[],"total":0}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, path, "", alice)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, m, decode[monitorJSON](t, rec.Body.Bytes()), "alice's monitor is untouched")
}

func TestMonitorsNotFound(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")

	for _, path := range []string{
		"/api/v1/monitors/6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f", // unknown
		"/api/v1/monitors/not-a-uuid",                           // malformed
	} {
		rec := api.do(t, http.MethodGet, path, "", token)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
	}
}

func TestMonitorsRequireAuth(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())

	for _, tt := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/monitors"},
		{http.MethodGet, "/api/v1/monitors"},
		{http.MethodGet, "/api/v1/monitors/6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f"},
		{http.MethodPatch, "/api/v1/monitors/6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f"},
		{http.MethodDelete, "/api/v1/monitors/6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f"},
	} {
		rec := api.do(t, tt.method, tt.path, `{}`, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tt.method, tt.path)
	}
}
