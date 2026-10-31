package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
)

// Recover turns a panic in a handler into a 500 response in the API error
// format and logs it with the stack trace and request ID. Without it,
// net/http would recover the panic itself, but only drop the connection and
// write a plain-text line to the error log.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// http.ErrAbortHandler is the documented way for a handler
				// to abort a response; net/http expects to see it and
				// suppresses the log. Pass it on.
				if v == http.ErrAbortHandler { //nolint:errorlint // compared by identity, as net/http does
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic in handler",
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				response.WriteError(w, http.StatusInternalServerError, response.CodeInternal, "Internal server error", nil)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
