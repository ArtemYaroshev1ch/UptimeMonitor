package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

// CheckRepository reads stored checks, scoped to the owner.
type CheckRepository interface {
	List(ctx context.Context, userID, monitorID uuid.UUID, from, to time.Time, limit, offset int) ([]domain.Check, int, error)
	Stats(ctx context.Context, userID, monitorID uuid.UUID, since time.Time) (domain.MonitorStats, error)
}

// MonitorGetter finds one monitor of a user (MonitorRepository has it).
type MonitorGetter interface {
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Monitor, error)
}

// defaultHistory is the range of GET .../checks without from/to.
const defaultHistory = 24 * time.Hour

// statsPeriod returns the length of a period GET .../stats accepts.
func statsPeriod(name string) (time.Duration, bool) {
	switch name {
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// CheckQuery selects a page of a monitor's history. Nil From/To default to
// the last 24 hours.
type CheckQuery struct {
	From, To      *time.Time
	Limit, Offset int
}

// CheckPage is one page of history.
type CheckPage struct {
	Items []domain.Check
	Total int
	From  time.Time
	To    time.Time
}

// PeriodStats is MonitorStats with the period it covers.
type PeriodStats struct {
	domain.MonitorStats
	Period string
	From   time.Time
}

// Checks serves the history and statistics of monitors.
type Checks struct {
	monitors MonitorGetter
	checks   CheckRepository
	clock    clock.Clock
}

// NewChecks returns the Checks service.
func NewChecks(monitors MonitorGetter, checks CheckRepository, clk clock.Clock) *Checks {
	return &Checks{monitors: monitors, checks: checks, clock: clk}
}

// List returns a page of the monitor's checks, newest first. Errors:
// domain.ErrNotFound (not the user's monitor), *domain.ValidationError.
func (s *Checks) List(ctx context.Context, userID, monitorID uuid.UUID, q CheckQuery) (CheckPage, error) {
	to := s.clock.Now()
	if q.To != nil {
		to = *q.To
	}
	from := to.Add(-defaultHistory)
	if q.From != nil {
		from = *q.From
	}
	if !from.Before(to) {
		return CheckPage{}, domain.NewValidationError("from", "must be before to")
	}

	// A missing monitor would just have no checks; asking first turns that
	// into the 404 the client should see.
	if _, err := s.monitors.Get(ctx, userID, monitorID); err != nil {
		return CheckPage{}, fmt.Errorf("list checks: %w", err)
	}
	items, total, err := s.checks.List(ctx, userID, monitorID, from, to, q.Limit, q.Offset)
	if err != nil {
		return CheckPage{}, fmt.Errorf("list checks: %w", err)
	}
	return CheckPage{Items: items, Total: total, From: from, To: to}, nil
}

// Stats aggregates the monitor's checks over period ("24h", "7d" or "30d";
// "" means 24h). Errors: domain.ErrNotFound, *domain.ValidationError.
func (s *Checks) Stats(ctx context.Context, userID, monitorID uuid.UUID, period string) (PeriodStats, error) {
	if period == "" {
		period = "24h"
	}
	length, ok := statsPeriod(period)
	if !ok {
		return PeriodStats{}, domain.NewValidationError("period", "must be one of: 24h, 7d, 30d")
	}

	if _, err := s.monitors.Get(ctx, userID, monitorID); err != nil {
		return PeriodStats{}, fmt.Errorf("monitor stats: %w", err)
	}
	from := s.clock.Now().Add(-length)
	stats, err := s.checks.Stats(ctx, userID, monitorID, from)
	if err != nil {
		return PeriodStats{}, fmt.Errorf("monitor stats: %w", err)
	}
	return PeriodStats{MonitorStats: stats, Period: period, From: from}, nil
}
