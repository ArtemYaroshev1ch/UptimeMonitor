package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/service"
)

// IncidentService is the part of service.Incidents the handlers use.
type IncidentService interface {
	List(ctx context.Context, userID uuid.UUID, f domain.IncidentFilter, limit, offset int) (service.IncidentPage, error)
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Incident, error)
}

// Incidents serves /api/v1/incidents.
type Incidents struct {
	svc      IncidentService
	monitors Monitors // for the authenticated user
	log      *slog.Logger
}

// NewIncidents returns the incident handlers.
func NewIncidents(svc IncidentService, log *slog.Logger) Incidents {
	return Incidents{svc: svc, monitors: Monitors{log: log}, log: log}
}

type incidentResponse struct {
	ID          uuid.UUID  `json:"id"`
	MonitorID   uuid.UUID  `json:"monitor_id"`
	MonitorName string     `json:"monitor_name"`
	StartedAt   time.Time  `json:"started_at"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	CauseKind   *string    `json:"cause_kind"`
	// DurationSeconds is resolved_at - started_at; null while open.
	DurationSeconds *int64 `json:"duration_seconds"`
}

func newIncidentResponse(inc domain.Incident) incidentResponse {
	res := incidentResponse{
		ID:          inc.ID,
		MonitorID:   inc.MonitorID,
		MonitorName: inc.MonitorName,
		StartedAt:   inc.StartedAt.UTC(),
		ResolvedAt:  utc(inc.ResolvedAt),
		CauseKind:   orNull(string(inc.CauseKind)),
	}
	if inc.ResolvedAt != nil {
		d := int64(inc.ResolvedAt.Sub(inc.StartedAt).Seconds())
		res.DurationSeconds = &d
	}
	return res
}

// List handles GET /api/v1/incidents?monitor_id=&open=&limit=&offset=.
func (h Incidents) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.monitors.user(w, r)
	if !ok {
		return
	}
	f, pg, err := parseIncidentQuery(r)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	page, err := h.svc.List(r.Context(), userID, f, pg.Limit, pg.Offset)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	items := make([]incidentResponse, 0, len(page.Items))
	for _, inc := range page.Items {
		items = append(items, newIncidentResponse(inc))
	}
	response.JSON(w, http.StatusOK, listResponse[incidentResponse]{Items: items, Total: page.Total})
}

// Get handles GET /api/v1/incidents/{id}. Unknown, malformed and foreign
// IDs are all 404.
func (h Incidents) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.monitors.user(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, h.log, fmt.Errorf("incident id: %w", domain.ErrNotFound))
		return
	}
	r = r.WithContext(logger.WithAttrs(r.Context(), slog.String("incident_id", id.String())))
	inc, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusOK, newIncidentResponse(inc))
}

// parseIncidentQuery reads monitor_id (UUID), open (true/false) and the
// page window.
func parseIncidentQuery(r *http.Request) (domain.IncidentFilter, page, error) {
	var f domain.IncidentFilter
	var details []domain.FieldError
	q := r.URL.Query()

	pg, err := parsePage(r)
	if verr, ok := errors.AsType[*domain.ValidationError](err); ok {
		details = append(details, verr.Details...)
	}

	if v := q.Get("monitor_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			details = append(details, domain.FieldError{Field: "monitor_id", Issue: "must be a UUID"})
		} else {
			f.MonitorID = &id
		}
	}
	switch v := q.Get("open"); v {
	case "":
	case "true", "false":
		open := v == "true"
		f.Open = &open
	default:
		details = append(details, domain.FieldError{Field: "open", Issue: "must be true or false"})
	}

	if len(details) > 0 {
		return domain.IncidentFilter{}, page{}, &domain.ValidationError{Details: details}
	}
	return f, pg, nil
}
