package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want string
	}{
		{addr: ":8080", want: "http://127.0.0.1:8080/healthz"},
		{addr: "0.0.0.0:9091", want: "http://127.0.0.1:9091/healthz"},
		{addr: "[::]:8080", want: "http://127.0.0.1:8080/healthz"},
		{addr: "localhost:8080", want: "http://localhost:8080/healthz"},
		{addr: "10.0.0.5:8080", want: "http://10.0.0.5:8080/healthz"},
		{addr: "[::1]:8080", want: "http://[::1]:8080/healthz"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()

			got, err := URL(tt.addr, "/healthz")

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestURLInvalidAddress(t *testing.T) {
	t.Parallel()

	_, err := URL("8080", "/healthz")

	require.Error(t, err)
}

func TestCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "healthy", status: http.StatusOK},
		{name: "unhealthy", status: http.StatusServiceUnavailable, wantErr: true},
		{name: "other 2xx is not healthy", status: http.StatusNoContent, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)

			err := Check(t.Context(), srv.URL+"/healthz")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckServerDown(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()

	require.Error(t, Check(ctx, url))
}
