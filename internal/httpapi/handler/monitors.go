package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/middleware"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/service"
)

// MonitorService is the part of service.Monitors the handlers use.
type MonitorService interface {
	Create(ctx context.Context, userID uuid.UUID, p service.MonitorPatch) (domain.Monitor, error)
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Monitor, error)
	List(ctx context.Context, userID uuid.UUID, limit, offset int) (service.MonitorPage, error)
	Update(ctx context.Context, userID, id uuid.UUID, p service.MonitorPatch) (domain.Monitor, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// Monitors serves /api/v1/monitors. All routes run behind
// middleware.Authenticate and act on the authenticated user's monitors only.
type Monitors struct {
	svc MonitorService
	log *slog.Logger
}

// NewMonitors returns the monitor handlers.
func NewMonitors(svc MonitorService, log *slog.Logger) Monitors {
	return Monitors{svc: svc, log: log}
}

type monitorResponse struct {
	ID                 uuid.UUID  `json:"id"`
	Name               string     `json:"name"`
	URL                string     `json:"url"`
	Method             string     `json:"method"`
	IntervalSeconds    int        `json:"interval_seconds"`
	TimeoutSeconds     int        `json:"timeout_seconds"`
	ExpectedStatus     *int       `json:"expected_status"`
	Keyword            *string    `json:"keyword"`
	FollowRedirects    bool       `json:"follow_redirects"`
	FailureThreshold   int        `json:"failure_threshold"`
	CertExpiryWarnDays int        `json:"cert_expiry_warn_days"`
	IsActive           bool       `json:"is_active"`
	Status             string     `json:"status"`
	TLSNotAfter        *time.Time `json:"tls_not_after"`
	LastCheckedAt      *time.Time `json:"last_checked_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func newMonitorResponse(m domain.Monitor) monitorResponse {
	return monitorResponse{
		ID:                 m.ID,
		Name:               m.Name,
		URL:                m.URL,
		Method:             m.Method,
		IntervalSeconds:    m.IntervalSeconds,
		TimeoutSeconds:     m.TimeoutSeconds,
		ExpectedStatus:     m.ExpectedStatus,
		Keyword:            m.Keyword,
		FollowRedirects:    m.FollowRedirects,
		FailureThreshold:   m.FailureThreshold,
		CertExpiryWarnDays: m.CertExpiryWarnDays,
		IsActive:           m.IsActive,
		Status:             string(m.Status),
		TLSNotAfter:        utc(m.TLSNotAfter),
		LastCheckedAt:      utc(m.LastCheckedAt),
		CreatedAt:          m.CreatedAt.UTC(),
		UpdatedAt:          m.UpdatedAt.UTC(),
	}
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// Create handles POST /api/v1/monitors.
func (h Monitors) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	var p service.MonitorPatch
	if err := decodeJSON(r, &p); err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	m, err := h.svc.Create(r.Context(), userID, p)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	h.log.InfoContext(r.Context(), "monitor created", slog.String("monitor_id", m.ID.String()))
	w.Header().Set("Location", "/api/v1/monitors/"+m.ID.String())
	response.JSON(w, http.StatusCreated, newMonitorResponse(m))
}

// List handles GET /api/v1/monitors.
func (h Monitors) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}
	pg, err := parsePage(r)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	res, err := h.svc.List(r.Context(), userID, pg.Limit, pg.Offset)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	items := make([]monitorResponse, 0, len(res.Items))
	for _, m := range res.Items {
		items = append(items, newMonitorResponse(m))
	}
	response.JSON(w, http.StatusOK, listResponse[monitorResponse]{Items: items, Total: res.Total})
}

// Get handles GET /api/v1/monitors/{id}.
func (h Monitors) Get(w http.ResponseWriter, r *http.Request) {
	userID, id, r, ok := h.userAndMonitor(w, r)
	if !ok {
		return
	}
	m, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusOK, newMonitorResponse(m))
}

// Update handles PATCH /api/v1/monitors/{id}.
func (h Monitors) Update(w http.ResponseWriter, r *http.Request) {
	userID, id, r, ok := h.userAndMonitor(w, r)
	if !ok {
		return
	}
	var p service.MonitorPatch
	if err := decodeJSON(r, &p); err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	m, err := h.svc.Update(r.Context(), userID, id, p)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusOK, newMonitorResponse(m))
}

// Delete handles DELETE /api/v1/monitors/{id}.
func (h Monitors) Delete(w http.ResponseWriter, r *http.Request) {
	userID, id, r, ok := h.userAndMonitor(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	h.log.InfoContext(r.Context(), "monitor deleted")
	w.WriteHeader(http.StatusNoContent)
}

// user returns the authenticated user or writes a 500: only a route without
// middleware.Authenticate gets here without one.
func (h Monitors) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		response.Error(w, r, h.log, fmt.Errorf("monitors: route is not authenticated"))
	}
	return userID, ok
}

// userAndMonitor also parses {id}. A malformed ID gets the same 404 as an
// unknown or foreign one: in every case "there is no such monitor of yours".
// The returned request's context logs monitor_id.
func (h Monitors) userAndMonitor(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, *http.Request, bool) {
	userID, ok := h.user(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, r, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, h.log, fmt.Errorf("monitor id: %w", domain.ErrNotFound))
		return uuid.Nil, uuid.Nil, r, false
	}
	ctx := logger.WithAttrs(r.Context(), slog.String("monitor_id", id.String()))
	return userID, id, r.WithContext(ctx), true
}
