package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/service"
)

// CheckService is the part of service.Checks the handlers use.
type CheckService interface {
	List(ctx context.Context, userID, monitorID uuid.UUID, q service.CheckQuery) (service.CheckPage, error)
	Stats(ctx context.Context, userID, monitorID uuid.UUID, period string) (service.PeriodStats, error)
}

// Checks serves the history and statistics of a monitor.
type Checks struct {
	svc      CheckService
	monitors Monitors // for user and {id} parsing
	log      *slog.Logger
}

// NewChecks returns the check handlers.
func NewChecks(svc CheckService, log *slog.Logger) Checks {
	return Checks{svc: svc, monitors: Monitors{log: log}, log: log}
}

type checkResponse struct {
	ID             int64      `json:"id"`
	CheckedAt      time.Time  `json:"checked_at"`
	Success        bool       `json:"success"`
	StatusCode     *int       `json:"status_code"`
	ResponseTimeMs int        `json:"response_time_ms"`
	DNSMs          *int       `json:"dns_ms"`
	ConnectMs      *int       `json:"connect_ms"`
	TLSMs          *int       `json:"tls_ms"`
	TTFBMs         *int       `json:"ttfb_ms"`
	HTTPProto      *string    `json:"http_proto"`
	TLSVersion     *string    `json:"tls_version"`
	TLSNotAfter    *time.Time `json:"tls_not_after"`
	ErrorKind      *string    `json:"error_kind"`
	ErrorMessage   *string    `json:"error_message"`
}

func newCheckResponse(c domain.Check) checkResponse {
	return checkResponse{
		ID:             c.ID,
		CheckedAt:      c.CheckedAt.UTC(),
		Success:        c.Success,
		StatusCode:     c.StatusCode,
		ResponseTimeMs: c.ResponseTimeMs,
		DNSMs:          c.DNSMs,
		ConnectMs:      c.ConnectMs,
		TLSMs:          c.TLSMs,
		TTFBMs:         c.TTFBMs,
		HTTPProto:      orNull(c.HTTPProto),
		TLSVersion:     orNull(c.TLSVersion),
		TLSNotAfter:    utc(c.TLSNotAfter),
		ErrorKind:      orNull(string(c.ErrorKind)),
		ErrorMessage:   orNull(c.ErrorMessage),
	}
}

// orNull renders "" as JSON null: "no value", not "an empty value".
func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

type checkListResponse struct {
	Items []checkResponse `json:"items"`
	Total int             `json:"total"`
	From  time.Time       `json:"from"`
	To    time.Time       `json:"to"`
}

type statsResponse struct {
	Period        string    `json:"period"`
	From          time.Time `json:"from"`
	TotalChecks   int       `json:"total_checks"`
	FailedChecks  int       `json:"failed_checks"`
	UptimePercent *float64  `json:"uptime_percent"`
	AvgResponseMs *float64  `json:"avg_response_ms"`
	P95ResponseMs *float64  `json:"p95_response_ms"`
	P95TTFBMs     *float64  `json:"p95_ttfb_ms"`
}

// List handles GET /api/v1/monitors/{id}/checks?from=&to=&limit=&offset=.
func (h Checks) List(w http.ResponseWriter, r *http.Request) {
	userID, monitorID, r, ok := h.monitors.userAndMonitor(w, r)
	if !ok {
		return
	}
	q, err := parseCheckQuery(r)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	page, err := h.svc.List(r.Context(), userID, monitorID, q)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	items := make([]checkResponse, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, newCheckResponse(c))
	}
	response.JSON(w, http.StatusOK, checkListResponse{
		Items: items, Total: page.Total, From: page.From.UTC(), To: page.To.UTC(),
	})
}

// Stats handles GET /api/v1/monitors/{id}/stats?period=24h|7d|30d.
func (h Checks) Stats(w http.ResponseWriter, r *http.Request) {
	userID, monitorID, r, ok := h.monitors.userAndMonitor(w, r)
	if !ok {
		return
	}
	s, err := h.svc.Stats(r.Context(), userID, monitorID, r.URL.Query().Get("period"))
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusOK, statsResponse{
		Period:        s.Period,
		From:          s.From.UTC(),
		TotalChecks:   s.TotalChecks,
		FailedChecks:  s.FailedChecks,
		UptimePercent: s.UptimePercent,
		AvgResponseMs: s.AvgResponseMs,
		P95ResponseMs: s.P95ResponseMs,
		P95TTFBMs:     s.P95TTFBMs,
	})
}

// parseCheckQuery reads from/to (RFC 3339) and the page window.
func parseCheckQuery(r *http.Request) (service.CheckQuery, error) {
	var q service.CheckQuery
	var details []domain.FieldError

	pg, err := parsePage(r)
	if verr, ok := errors.AsType[*domain.ValidationError](err); ok {
		details = append(details, verr.Details...)
	}
	q.Limit, q.Offset = pg.Limit, pg.Offset

	for _, f := range []struct {
		name string
		dst  **time.Time
	}{{"from", &q.From}, {"to", &q.To}} {
		v := r.URL.Query().Get(f.name)
		if v == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			details = append(details, domain.FieldError{Field: f.name, Issue: "must be an RFC 3339 time, e.g. 2026-01-02T15:04:05Z"})
			continue
		}
		*f.dst = &t
	}

	if len(details) > 0 {
		return service.CheckQuery{}, &domain.ValidationError{Details: details}
	}
	return q, nil
}
