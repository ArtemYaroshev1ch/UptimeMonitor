package response

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

func TestError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
		wantLogged bool
	}{
		{
			name:       "validation",
			err:        fmt.Errorf("register: %w", domain.NewValidationError("email", "is required")),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"code":"validation_failed","message":"Invalid request","details":[{"field":"email","issue":"is required"}]}}`,
		},
		{
			name:       "unauthorized",
			err:        fmt.Errorf("login: %w", domain.ErrUnauthorized),
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"error":{"code":"unauthorized","message":"Authentication required"}}`,
		},
		{
			name:       "not found",
			err:        fmt.Errorf("get: %w", domain.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":{"code":"not_found","message":"Resource not found"}}`,
		},
		{
			name:       "conflict",
			err:        fmt.Errorf("create: %w", domain.ErrConflict),
			wantStatus: http.StatusConflict,
			wantBody:   `{"error":{"code":"conflict","message":"Resource already exists"}}`,
		},
		{
			// The cause stays in the log; the client learns nothing about it.
			name:       "internal",
			err:        errors.New("dial tcp 10.0.0.5:5432: connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":{"code":"internal","message":"Internal server error"}}`,
			wantLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

			Error(rec, req, log, tt.err)

			assert.Equal(t, tt.wantStatus, rec.Code)
			assert.JSONEq(t, tt.wantBody, rec.Body.String())
			if tt.wantLogged {
				assert.Contains(t, logs.String(), "connection refused")
			} else {
				assert.Empty(t, logs.String())
			}
		})
	}
}

func TestErrorUnauthorizedChallenge(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

	Error(rec, req, slog.New(slog.DiscardHandler), domain.ErrUnauthorized)

	assert.Equal(t, `Bearer realm="uptime-monitor"`, rec.Header().Get("WWW-Authenticate"))
}
