package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
)

// readyzTimeout bounds a readiness check. Orchestrators probe with a timeout
// of their own (a few seconds); answering 503 before it expires is more
// useful than letting the probe time out.
const readyzTimeout = 2 * time.Second

// Pinger checks that a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

type readyzResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// Readyz is the readiness probe: 200 when the instance can serve requests,
// 503 when the database is unreachable. A failing readiness probe takes the
// instance out of the load balancer without restarting it. The response
// names the failed check but not the error: this port is public, the details
// go to the log.
func Readyz(db Pinger, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyzTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			log.WarnContext(ctx, "readiness check failed",
				slog.String("check", "database"),
				slog.Any("error", err),
			)
			response.JSON(w, http.StatusServiceUnavailable, readyzResponse{
				Status: "unavailable",
				Checks: map[string]string{"database": "unavailable"},
			})
			return
		}

		response.JSON(w, http.StatusOK, readyzResponse{
			Status: "ok",
			Checks: map[string]string{"database": "ok"},
		})
	}
}
