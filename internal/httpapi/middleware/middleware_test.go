package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
)

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
}

// jsonLogger returns a logger at debug level that writes JSON into buf,
// through the project logger, so context attributes are included.
func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return logger.New(buf, logger.Options{Format: logger.FormatJSON, Level: slog.LevelDebug})
}

// logRecords parses JSON log lines.
func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		out = append(out, rec)
	}
	return out
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		wantSame bool
	}{
		{name: "generated", incoming: ""},
		{name: "propagated", incoming: "abc-123_DEF.4", wantSame: true},
		{name: "too long", incoming: strings.Repeat("a", 65)},
		{name: "unsafe characters", incoming: "abc\nlevel=ERROR msg=forged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var seen string
			h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = GetRequestID(r.Context())
			}))
			req := newRequest(t, http.MethodGet, "/")
			if tt.incoming != "" {
				req.Header.Set(RequestIDHeader, tt.incoming)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			got := rec.Header().Get(RequestIDHeader)
			assert.Equal(t, seen, got, "the handler and the client see the same ID")
			if tt.wantSame {
				assert.Equal(t, tt.incoming, got)
			} else {
				assert.Len(t, got, 26)
				assert.NotEqual(t, tt.incoming, got)
			}
		})
	}
}

func TestRequestIDIsUnique(t *testing.T) {
	t.Parallel()
	h := RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	ids := map[string]bool{}
	for range 100 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newRequest(t, http.MethodGet, "/"))
		ids[rec.Header().Get(RequestIDHeader)] = true
	}

	assert.Len(t, ids, 100)
}

func TestRecover(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	h := RequestID(Recover(jsonLogger(&logs))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, newRequest(t, http.MethodGet, "/"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"internal","message":"Internal server error"}}`, rec.Body.String())
	records := logRecords(t, &logs)
	require.Len(t, records, 1)
	assert.Equal(t, "boom", records[0]["panic"])
	assert.Contains(t, records[0]["stack"], "TestRecover")
	assert.Equal(t, rec.Header().Get(RequestIDHeader), records[0]["request_id"])
}

func TestRecoverPassesAbortHandler(t *testing.T) {
	t.Parallel()
	h := Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		h.ServeHTTP(httptest.NewRecorder(), newRequest(t, http.MethodGet, "/"))
	})
}

func TestLogging(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		status    int
		wantLevel string
		wantRoute string
	}{
		{name: "success", path: "/items/42", status: http.StatusCreated, wantLevel: "INFO", wantRoute: "/items/{id}"},
		{name: "server error", path: "/items/42", status: http.StatusBadGateway, wantLevel: "ERROR", wantRoute: "/items/{id}"},
		{name: "probe", path: "/healthz", status: http.StatusOK, wantLevel: "DEBUG", wantRoute: "/healthz"},
		{name: "unknown route", path: "/nope", status: http.StatusNotFound, wantLevel: "INFO", wantRoute: "unmatched"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			r := chi.NewRouter()
			r.Use(RequestID, Logging(jsonLogger(&logs), clk))
			write := func(w http.ResponseWriter, _ *http.Request) {
				clk.Advance(1500 * time.Microsecond)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, "hello")
			}
			r.Get("/items/{id}", write)
			r.Get("/healthz", write)
			r.NotFound(write)

			r.ServeHTTP(httptest.NewRecorder(), newRequest(t, http.MethodGet, tt.path))

			records := logRecords(t, &logs)
			require.Len(t, records, 1)
			rec := records[0]
			assert.Equal(t, "http request", rec["msg"])
			assert.Equal(t, tt.wantLevel, rec["level"])
			assert.Equal(t, "GET", rec["method"])
			assert.Equal(t, tt.path, rec["path"])
			assert.Equal(t, tt.wantRoute, rec["route"])
			assert.InDelta(t, tt.status, rec["status"], 0)
			assert.InDelta(t, 5, rec["bytes"], 0)
			assert.InDelta(t, 1.5, rec["duration_ms"], 0)
			assert.NotEmpty(t, rec["request_id"])
		})
	}
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()
	var readErr error
	h := BodyLimit(10)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 11)))

	h.ServeHTTP(httptest.NewRecorder(), req)

	_, ok := errors.AsType[*http.MaxBytesError](readErr)
	assert.True(t, ok, "want *http.MaxBytesError, got %v", readErr)
}

// fakeVerifier accepts exactly one token.
type fakeVerifier struct {
	token  string
	userID uuid.UUID
}

func (f fakeVerifier) Verify(token string) (uuid.UUID, error) {
	if token != f.token {
		return uuid.Nil, errors.New("bad token")
	}
	return f.userID, nil
}

func TestAuthenticate(t *testing.T) {
	t.Parallel()
	userID := uuid.MustParse("6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f")
	verifier := fakeVerifier{token: "good-token", userID: userID}

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "valid", header: "Bearer good-token", wantStatus: http.StatusOK},
		{name: "scheme is case-insensitive", header: "bearer good-token", wantStatus: http.StatusOK},
		{name: "missing header", header: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Basic good-token", wantStatus: http.StatusUnauthorized},
		{name: "empty token", header: "Bearer ", wantStatus: http.StatusUnauthorized},
		{name: "invalid token", header: "Bearer forged", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotID uuid.UUID
			var logs bytes.Buffer
			log := jsonLogger(&logs)
			h := Authenticate(verifier, log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				id, ok := UserID(r.Context())
				require.True(t, ok)
				gotID = id
				log.InfoContext(r.Context(), "inside")
			}))
			req := newRequest(t, http.MethodGet, "/")
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusOK {
				assert.Equal(t, userID, gotID)
				assert.Contains(t, logs.String(), `"user_id":"`+userID.String()+`"`)
				return
			}
			assert.JSONEq(t, `{"error":{"code":"unauthorized","message":"Authentication required"}}`, rec.Body.String())
			assert.NotEmpty(t, rec.Header().Get("WWW-Authenticate"))
			assert.NotContains(t, logs.String(), "forged", "tokens are never logged")
		})
	}
}

func TestUserIDWithoutAuthenticate(t *testing.T) {
	t.Parallel()

	_, ok := UserID(t.Context())

	assert.False(t, ok)
}

func TestRateLimiter(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewRateLimiter(10, time.Minute, clk)

	for i := range 10 {
		ok, _ := l.Allow("1.2.3.4")
		require.True(t, ok, "request %d is within the burst", i+1)
	}
	ok, retryAfter := l.Allow("1.2.3.4")
	require.False(t, ok, "the 11th request is limited")
	assert.Equal(t, 6*time.Second, retryAfter, "one token every 60s/10")

	ok, _ = l.Allow("5.6.7.8")
	assert.True(t, ok, "other clients have their own bucket")

	clk.Advance(6 * time.Second)
	ok, _ = l.Allow("1.2.3.4")
	assert.True(t, ok, "a token has refilled")
	ok, _ = l.Allow("1.2.3.4")
	assert.False(t, ok, "but only one")
}

func TestRateLimiterSweepsIdleBuckets(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	l := NewRateLimiter(10, time.Minute, clk)
	for i := range 50 {
		l.Allow(fmt.Sprintf("10.0.0.%d", i))
	}
	require.Len(t, l.buckets, 50)

	clk.Advance(time.Minute)
	l.Allow("192.0.2.1")

	assert.Len(t, l.buckets, 1, "only the bucket used in the last window is kept")
}

func TestRateLimit(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	h := RateLimit(NewRateLimiter(2, time.Minute, clk), ClientIP(false))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	codes := make([]int, 0, 3)
	var last *httptest.ResponseRecorder
	for range 3 {
		last = httptest.NewRecorder()
		req := newRequest(t, http.MethodPost, "/")
		req.RemoteAddr = "192.0.2.1:5555"
		h.ServeHTTP(last, req)
		codes = append(codes, last.Code)
	}

	assert.Equal(t, []int{http.StatusNoContent, http.StatusNoContent, http.StatusTooManyRequests}, codes)
	assert.Equal(t, "30", last.Header().Get("Retry-After"))
	assert.JSONEq(t, `{"error":{"code":"rate_limited","message":"Too many requests"}}`, last.Body.String())
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trustProxy bool
		remoteAddr string
		xff        []string
		want       string
	}{
		{name: "peer address", remoteAddr: "192.0.2.1:5555", want: "192.0.2.1"},
		{name: "IPv6 peer", remoteAddr: "[2001:db8::1]:5555", want: "2001:db8::1"},
		{
			// Without a trusted proxy the header is the client's own claim.
			name:       "header ignored by default",
			remoteAddr: "192.0.2.1:5555",
			xff:        []string{"203.0.113.9"},
			want:       "192.0.2.1",
		},
		{
			name:       "last entry appended by the proxy",
			trustProxy: true,
			remoteAddr: "10.0.0.2:5555",
			xff:        []string{"1.1.1.1, 203.0.113.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "last of several headers",
			trustProxy: true,
			remoteAddr: "10.0.0.2:5555",
			xff:        []string{"1.1.1.1", "203.0.113.9"},
			want:       "203.0.113.9",
		},
		{
			name:       "garbage header falls back to the peer",
			trustProxy: true,
			remoteAddr: "10.0.0.2:5555",
			xff:        []string{"not-an-ip"},
			want:       "10.0.0.2",
		},
		{
			name:       "no header",
			trustProxy: true,
			remoteAddr: "10.0.0.2:5555",
			want:       "10.0.0.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := newRequest(t, http.MethodGet, "/")
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}

			assert.Equal(t, tt.want, ClientIP(tt.trustProxy)(req))
		})
	}
}
