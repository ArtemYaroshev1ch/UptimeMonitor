// Package httpapi wires the public HTTP API: routes, middleware and handlers.
package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/handler"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
)

// Deps holds everything the router needs to build handlers.
type Deps struct {
	BuildInfo observability.BuildInfo
}

// NewRouter returns the handler of the public API port.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", handler.Healthz)
	r.Get("/version", handler.Version(d.BuildInfo))

	return r
}
