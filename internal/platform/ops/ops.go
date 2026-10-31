// Package ops builds the handler of an ops port: the internal endpoints that
// orchestrators and Prometheus use (never published outside the host
// network). The worker serves /healthz and /readyz here; /metrics and pprof
// join in Phase 7.
package ops

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// readyTimeout bounds all readiness checks together, below the few seconds
// an orchestrator waits for a probe.
const readyTimeout = 2 * time.Second

// Check is one readiness condition, e.g. "database" or "scheduler".
type Check struct {
	Name string
	// Fn returns nil when the condition holds.
	Fn func(ctx context.Context) error
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Handler serves:
//
//	GET /healthz  200 while the process runs; checks nothing (liveness)
//	GET /readyz   200 if every check passes, else 503 (readiness)
//
// Readiness responses name the failed checks; the errors go to the log.
func Handler(log *slog.Logger, checks ...Check) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		res := readyResponse{Status: "ok", Checks: make(map[string]string, len(checks))}
		code := http.StatusOK
		for _, c := range checks {
			if err := c.Fn(ctx); err != nil {
				log.WarnContext(ctx, "readiness check failed", slog.String("check", c.Name), slog.Any("error", err))
				res.Checks[c.Name] = "unavailable"
				res.Status = "unavailable"
				code = http.StatusServiceUnavailable
				continue
			}
			res.Checks[c.Name] = "ok"
		}
		writeJSON(w, code, res)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encoding these fixed types cannot fail; a write error means the
	// client is gone.
	_ = json.NewEncoder(w).Encode(v)
}
