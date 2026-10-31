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

func TestMonitorChecks(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	m := api.createMonitor(t, token, `{"name":"api","url":"https://example.com"}`)
	now := api.clock.Now()
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.MustParse(m.ID)
	api.checks.Add(id, domain.CheckResult{
		CheckedAt: now.Add(-time.Minute), Success: true, StatusCode: new(200), ResponseTimeMs: 120,
		DNSMs: new(5), ConnectMs: new(10), TLSMs: new(30), TTFBMs: new(70),
		HTTPProto: "HTTP/2.0", TLSVersion: "TLS1.3", TLSNotAfter: &notAfter,
	})
	api.checks.Add(id, domain.CheckResult{
		CheckedAt: now.Add(-2 * time.Minute), ResponseTimeMs: 10000,
		ErrorKind: domain.ErrorTimeout, ErrorMessage: "context deadline exceeded",
	})

	rec := api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+"/checks", "", token)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{
		"items": [
			{"id": 1, "checked_at": "2026-01-02T03:03:05Z", "success": true, "status_code": 200,
			 "response_time_ms": 120, "dns_ms": 5, "connect_ms": 10, "tls_ms": 30, "ttfb_ms": 70,
			 "http_proto": "HTTP/2.0", "tls_version": "TLS1.3", "tls_not_after": "2027-01-01T00:00:00Z",
			 "error_kind": null, "error_message": null},
			{"id": 2, "checked_at": "2026-01-02T03:02:05Z", "success": false, "status_code": null,
			 "response_time_ms": 10000, "dns_ms": null, "connect_ms": null, "tls_ms": null, "ttfb_ms": null,
			 "http_proto": null, "tls_version": null, "tls_not_after": null,
			 "error_kind": "timeout", "error_message": "context deadline exceeded"}
		],
		"total": 2,
		"from": "2026-01-01T03:04:05Z",
		"to": "2026-01-02T03:04:05Z"
	}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+
		"/checks?from=2026-01-02T03:02:30Z&to=2026-01-02T03:04:00Z", "", token)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	page := decode[struct {
		Total int `json:"total"`
	}](t, rec.Body.Bytes())
	assert.Equal(t, 1, page.Total)
}

func TestMonitorChecksQueryErrors(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	m := api.createMonitor(t, token, `{"name":"api","url":"https://example.com"}`)

	rec := api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+"/checks?from=yesterday&limit=0", "", token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"limit","issue":"must be between 1 and 200"},
		{"field":"from","issue":"must be an RFC 3339 time, e.g. 2026-01-02T15:04:05Z"}]}}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+
		"/checks?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", "", token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "must be before to")
}

func TestMonitorStats(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	token := api.userToken(t, "alice@example.com")
	m := api.createMonitor(t, token, `{"name":"api","url":"https://example.com"}`)
	id := uuid.MustParse(m.ID)
	now := api.clock.Now()

	rec := api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+"/stats", "", token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"period":"24h","from":"2026-01-01T03:04:05Z","total_checks":0,"failed_checks":0,
		"uptime_percent":null,"avg_response_ms":null,"p95_response_ms":null,"p95_ttfb_ms":null}`,
		rec.Body.String(), "no checks yet: nulls, not zeros")

	api.checks.Add(id, domain.CheckResult{CheckedAt: now.Add(-time.Hour), Success: true, ResponseTimeMs: 100, TTFBMs: new(40)})
	api.checks.Add(id, domain.CheckResult{CheckedAt: now.Add(-2 * time.Hour), ResponseTimeMs: 5000})

	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+"/stats?period=7d", "", token)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"period":"7d","from":"2025-12-26T03:04:05Z","total_checks":2,"failed_checks":1,
		"uptime_percent":50,"avg_response_ms":100,"p95_response_ms":100,"p95_ttfb_ms":40}`, rec.Body.String())

	rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+"/stats?period=1y", "", token)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"validation_failed","message":"Invalid request","details":[
		{"field":"period","issue":"must be one of: 24h, 7d, 30d"}]}}`, rec.Body.String())
}

func TestMonitorChecksIsolation(t *testing.T) {
	t.Parallel()
	api := newTestAPI(t, fakeDB{}, discardLog())
	alice := api.userToken(t, "alice@example.com")
	mallory := api.userToken(t, "mallory@example.com")
	m := api.createMonitor(t, alice, `{"name":"api","url":"https://example.com"}`)
	api.checks.Add(uuid.MustParse(m.ID), domain.CheckResult{CheckedAt: api.clock.Now().Add(-time.Minute), Success: true})

	for _, path := range []string{"/checks", "/stats"} {
		rec := api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+path, "", mallory)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)

		rec = api.do(t, http.MethodGet, "/api/v1/monitors/"+m.ID+path, "", "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, path)
	}
}
