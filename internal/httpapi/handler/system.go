// Package handler contains HTTP handlers of the public API.
package handler

import (
	"net/http"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/observability"
)

// Healthz is the liveness probe: the process is up and serving HTTP. It
// deliberately does not touch the database. If it did, a database outage
// would make the orchestrator restart every healthy API instance, which
// cannot fix the database and only adds load. Dependencies belong in /readyz.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type versionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

// Version reports which build is running.
func Version(info observability.BuildInfo) http.HandlerFunc {
	body := versionResponse{
		Version: info.Version,
		Commit:  info.Commit,
		BuiltAt: info.BuiltAt,
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		response.JSON(w, http.StatusOK, body)
	}
}
