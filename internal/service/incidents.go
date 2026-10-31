package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// IncidentRepository reads incidents, scoped to the owner.
type IncidentRepository interface {
	List(ctx context.Context, userID uuid.UUID, f domain.IncidentFilter, limit, offset int) ([]domain.Incident, int, error)
	// Get returns domain.ErrNotFound for unknown and foreign incidents.
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Incident, error)
}

// IncidentPage is one page of incidents.
type IncidentPage struct {
	Items []domain.Incident
	Total int
}

// Incidents serves the incident history. Incidents are opened and resolved
// by the worker (see repository/postgres.Checks.SaveResult); the API only
// reads them.
type Incidents struct {
	repo IncidentRepository
}

// NewIncidents returns the Incidents service.
func NewIncidents(repo IncidentRepository) *Incidents {
	return &Incidents{repo: repo}
}

// List returns a page of the user's incidents, newest first.
func (s *Incidents) List(ctx context.Context, userID uuid.UUID, f domain.IncidentFilter, limit, offset int) (IncidentPage, error) {
	items, total, err := s.repo.List(ctx, userID, f, limit, offset)
	if err != nil {
		return IncidentPage{}, fmt.Errorf("list incidents: %w", err)
	}
	return IncidentPage{Items: items, Total: total}, nil
}

// Get returns one of the user's incidents or domain.ErrNotFound.
func (s *Incidents) Get(ctx context.Context, userID, id uuid.UUID) (domain.Incident, error) {
	inc, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return domain.Incident{}, fmt.Errorf("get incident: %w", err)
	}
	return inc, nil
}
