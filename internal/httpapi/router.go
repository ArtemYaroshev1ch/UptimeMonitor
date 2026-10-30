// Package httpapi wires the public HTTP API: routes, middleware and handlers.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/handler"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
)

// Deps holds everything the router needs to build handlers.
type Deps struct {
	BuildInfo observability.BuildInfo
	// DB is checked by /readyz.
	DB  handler.Pinger
	Log *slog.Logger
}

// NewRouter returns the handler of the public API port.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", handler.Healthz)
	r.Get("/readyz", handler.Readyz(d.DB, d.Log))
	r.Get("/version", handler.Version(d.BuildInfo))

	return r
}
