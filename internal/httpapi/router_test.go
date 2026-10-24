package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
)

func TestRouter(t *testing.T) {
	t.Parallel()

	router := NewRouter(Deps{
		BuildInfo: observability.BuildInfo{
			Version: "1.2.3",
			Commit:  "abc123",
			BuiltAt: "2026-01-02T03:04:05Z",
		},
	})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string // JSON; empty means "don't check"
	}{
		{
			name:       "liveness",
			method:     http.MethodGet,
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "version",
			method:     http.MethodGet,
			path:       "/version",
			wantStatus: http.StatusOK,
			wantBody:   `{"version":"1.2.3","commit":"abc123","built_at":"2026-01-02T03:04:05Z"}`,
		},
		{
			name:       "wrong method",
			method:     http.MethodPost,
			path:       "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown route",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			// Metrics live on the ops port only, never on the public one.
			name:       "metrics are not public",
			method:     http.MethodGet,
			path:       "/metrics",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, http.NoBody)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantBody != "" {
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				assert.JSONEq(t, tt.wantBody, rec.Body.String())
			}
		})
	}
}
