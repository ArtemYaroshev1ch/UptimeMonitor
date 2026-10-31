package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
)

// These tests use t.Setenv, which forbids t.Parallel: the environment is
// process-wide.

var testInfo = observability.BuildInfo{
	Version:   "1.2.3",
	Commit:    "abc123",
	BuiltAt:   "2026-01-02T03:04:05Z",
	GoVersion: "go1.27.0",
}

// setEnv isolates a test from variables set on the developer's machine.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	defaults := map[string]string{
		"APP_ENV":          "prod",
		"LOG_LEVEL":        "info",
		"HTTP_ADDR":        "127.0.0.1:0",
		"SHUTDOWN_TIMEOUT": "5s",
		// Nothing listens on port 1. The pool connects lazily, so the server
		// starts anyway; only /readyz would notice.
		"DATABASE_URL": "postgres://uptime:uptime@127.0.0.1:1/uptime",
		"JWT_SECRET":   "0123456789abcdef0123456789abcdef",
	}
	for k, v := range defaults {
		t.Setenv(k, v)
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestRunVersion(t *testing.T) {
	var out strings.Builder

	err := run(t.Context(), []string{"-version"}, testInfo, &out)

	require.NoError(t, err)
	assert.Equal(t, "api 1.2.3 (commit abc123, built 2026-01-02T03:04:05Z, go1.27.0)\n", out.String())
}

func TestRunInvalidConfig(t *testing.T) {
	setEnv(t, map[string]string{"APP_ENV": "staging"})

	err := run(t.Context(), nil, testInfo, io.Discard)

	require.ErrorContains(t, err, "APP_ENV")
}

func TestRunHealthcheck(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "healthy", status: http.StatusOK},
		{name: "unhealthy", status: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/healthz" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)
			setEnv(t, map[string]string{"HTTP_ADDR": srv.Listener.Addr().String()})

			err := run(t.Context(), []string{"-healthcheck"}, testInfo, io.Discard)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestRunServesUntilCancelled starts the real server on a free port, calls
// /healthz and then stops it the way a SIGTERM would: by cancelling ctx.
func TestRunServesUntilCancelled(t *testing.T) {
	setEnv(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	logs, logWriter := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(ctx, nil, testInfo, logWriter)
		_ = logWriter.Close()
		done <- err
	}()

	addr := waitForListenAddr(t, logs)
	go func() { _, _ = io.Copy(io.Discard, logs) }() // keep the pipe flowing

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/healthz", http.NoBody)
	require.NoError(t, err)
	// No keep-alive, so no spare connection delays the shutdown below (see
	// the same client in cmd/worker/main_test.go).
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
}

// waitForListenAddr reads JSON log lines until the server reports its address.
func waitForListenAddr(t *testing.T, logs io.Reader) string {
	t.Helper()
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		var record struct {
			Msg  string `json:"msg"`
			Addr string `json:"addr"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &record), "log line: %s", scanner.Text())
		if record.Msg == "http server listening" {
			return record.Addr
		}
	}
	t.Fatalf("server exited before listening: %v", scanner.Err())
	return ""
}
