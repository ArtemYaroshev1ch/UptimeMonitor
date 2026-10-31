package memory

import (
	"cmp"
	"context"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// Checks is an in-memory check repository. Ownership comes from the
// Monitors repository it is created with, like the JOIN in PostgreSQL.
type Checks struct {
	failable
	monitors *Monitors
	checks   []domain.Check
	nextID   int64
}

// NewChecks returns an empty Checks whose ownership checks use monitors.
func NewChecks(monitors *Monitors) *Checks {
	return &Checks{monitors: monitors}
}

// Add stores a check result of a monitor (test setup).
func (r *Checks) Add(monitorID uuid.UUID, res domain.CheckResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	r.checks = append(r.checks, domain.Check{ID: r.nextID, MonitorID: monitorID, CheckResult: res})
}

func (r *Checks) owned(userID, monitorID uuid.UUID) bool {
	r.monitors.mu.Lock()
	defer r.monitors.mu.Unlock()
	return r.monitors.index(userID, monitorID) >= 0
}

// List returns a page of the monitor's checks in [from, to), newest first.
func (r *Checks) List(_ context.Context, userID, monitorID uuid.UUID, from, to time.Time, limit, offset int) ([]domain.Check, int, error) {
	owned := r.owned(userID, monitorID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, 0, r.err
	}
	if !owned {
		return nil, 0, nil
	}
	var in []domain.Check
	for _, c := range r.checks {
		if c.MonitorID == monitorID && !c.CheckedAt.Before(from) && c.CheckedAt.Before(to) {
			in = append(in, c)
		}
	}
	slices.SortFunc(in, func(a, b domain.Check) int {
		return cmp.Or(b.CheckedAt.Compare(a.CheckedAt), cmp.Compare(b.ID, a.ID))
	})
	total := len(in)
	return in[min(offset, total):min(offset+limit, total)], total, nil
}

// Stats computes the same figures as the SQL query, including
// percentile_cont's linear interpolation and rounding.
func (r *Checks) Stats(_ context.Context, userID, monitorID uuid.UUID, since time.Time) (domain.MonitorStats, error) {
	owned := r.owned(userID, monitorID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.MonitorStats{}, r.err
	}
	var s domain.MonitorStats
	if !owned {
		return s, nil
	}
	var times, ttfbs []float64
	for _, c := range r.checks {
		if c.MonitorID != monitorID || c.CheckedAt.Before(since) {
			continue
		}
		s.TotalChecks++
		if !c.Success {
			s.FailedChecks++
			continue
		}
		times = append(times, float64(c.ResponseTimeMs))
		if c.TTFBMs != nil {
			ttfbs = append(ttfbs, float64(*c.TTFBMs))
		}
	}
	if s.TotalChecks > 0 {
		s.UptimePercent = ptr(round(100*float64(s.TotalChecks-s.FailedChecks)/float64(s.TotalChecks), 3))
	}
	if len(times) > 0 {
		var sum float64
		for _, v := range times {
			sum += v
		}
		s.AvgResponseMs = ptr(round(sum/float64(len(times)), 1))
		s.P95ResponseMs = ptr(round(percentile(times, 0.95), 1))
	}
	if len(ttfbs) > 0 {
		s.P95TTFBMs = ptr(round(percentile(ttfbs, 0.95), 1))
	}
	return s, nil
}

// percentile interpolates between the closest ranks, as percentile_cont.
func percentile(values []float64, p float64) float64 {
	slices.Sort(values)
	pos := p * float64(len(values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return values[lo] + (pos-float64(lo))*(values[hi]-values[lo])
}

func round(v float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(v*scale) / scale
}

func ptr[T any](v T) *T { return &v }
