// Package middleware contains the HTTP middleware of the public API. The
// order in which the router applies them matters; see httpapi.NewRouter.
package middleware

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
)

// RequestIDHeader carries the request ID in both directions: a proxy may set
// it on the request, and the API always returns it in the response, so a
// user can quote it in a bug report and an operator can find the logs.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLen bounds an incoming ID: it is copied into every log line.
const maxRequestIDLen = 64

type requestIDKey struct{}

// RequestID assigns each request an ID: the incoming X-Request-ID if it looks
// sane, otherwise a fresh random one. The ID is put into the response header,
// the request context (see GetRequestID) and every log record written with
// that context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			// 26 base32 characters, 128 random bits.
			id = rand.Text()
		}
		w.Header().Set(RequestIDHeader, id)

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ctx = logger.WithAttrs(ctx, slog.String("request_id", id))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetRequestID returns the ID assigned by RequestID, or "".
func GetRequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// validRequestID accepts short IDs of letters, digits and "-_.". Anything
// else (newlines, quotes, kilobytes of text) could forge or bloat log lines.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range []byte(id) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
