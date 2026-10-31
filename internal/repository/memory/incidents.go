package memory

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// Incidents is an in-memory incident repository. Ownership and monitor names
// come from the Monitors repository it is created with, like the JOIN in
// PostgreSQL.
type Incidents struct {
	failable
	monitors  *Monitors
	incidents []domain.Incident
}

// NewIncidents returns an empty Incidents backed by monitors.
func NewIncidents(monitors *Monitors) *Incidents {
	return &Incidents{monitors: monitors}
}

// Add stores an incident as is (test setup). A zero ID gets a new one.
func (r *Incidents) Add(inc domain.Incident) domain.Incident {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inc.ID == uuid.Nil {
		inc.ID = uuid.New()
	}
	r.incidents = append(r.incidents, inc)
	return inc
}

// owner returns the owner and name of a monitor, ok=false if it is gone.
func (r *Incidents) owner(monitorID uuid.UUID) (userID uuid.UUID, name string, ok bool) {
	r.monitors.mu.Lock()
	defer r.monitors.mu.Unlock()
	i := slices.IndexFunc(r.monitors.monitors, func(m domain.Monitor) bool { return m.ID == monitorID })
	if i < 0 {
		return uuid.Nil, "", false
	}
	m := r.monitors.monitors[i]
	return m.UserID, m.Name, true
}

// own returns the user's incidents with monitor names, newest first.
func (r *Incidents) own(userID uuid.UUID) []domain.Incident {
	r.mu.Lock()
	all := slices.Clone(r.incidents)
	r.mu.Unlock()

	var out []domain.Incident
	for _, inc := range all {
		owner, name, ok := r.owner(inc.MonitorID)
		if !ok || owner != userID {
			continue
		}
		inc.MonitorName = name
		out = append(out, inc)
	}
	slices.SortFunc(out, func(a, b domain.Incident) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID.String(), a.ID.String()))
	})
	return out
}

// List returns a page of the user's incidents matching f and their total.
func (r *Incidents) List(_ context.Context, userID uuid.UUID, f domain.IncidentFilter, limit, offset int) ([]domain.Incident, int, error) {
	if err := r.failure(); err != nil {
		return nil, 0, err
	}
	var match []domain.Incident
	for _, inc := range r.own(userID) {
		if f.MonitorID != nil && inc.MonitorID != *f.MonitorID {
			continue
		}
		if f.Open != nil && inc.Open() != *f.Open {
			continue
		}
		match = append(match, inc)
	}
	total := len(match)
	return match[min(offset, total):min(offset+limit, total)], total, nil
}

// Get returns one of the user's incidents or domain.ErrNotFound.
func (r *Incidents) Get(_ context.Context, userID, id uuid.UUID) (domain.Incident, error) {
	if err := r.failure(); err != nil {
		return domain.Incident{}, err
	}
	for _, inc := range r.own(userID) {
		if inc.ID == id {
			return inc, nil
		}
	}
	return domain.Incident{}, domain.ErrNotFound
}
