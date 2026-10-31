package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// Incidents reads incidents for the API. Incidents are opened and resolved
// by Checks.SaveResult, in the transaction that stores the check.
type Incidents struct {
	db DB
}

// NewIncidents returns an Incidents repository.
func NewIncidents(db DB) *Incidents {
	return &Incidents{db: db}
}

const incidentColumns = `i.id, i.monitor_id, i.started_at, i.resolved_at, i.cause_kind`

// scanIncident reads incidentColumns.
func scanIncident(row pgx.Row) (domain.Incident, error) {
	var inc domain.Incident
	var cause *string
	err := row.Scan(&inc.ID, &inc.MonitorID, &inc.StartedAt, &inc.ResolvedAt, &cause)
	inc.CauseKind = domain.ErrorKind(deref(cause))
	return inc, err
}

// scanIncidentWithName reads incidentColumns followed by m.name.
func scanIncidentWithName(row pgx.Row) (domain.Incident, error) {
	var inc domain.Incident
	var cause *string
	err := row.Scan(&inc.ID, &inc.MonitorID, &inc.StartedAt, &inc.ResolvedAt, &cause, &inc.MonitorName)
	inc.CauseKind = domain.ErrorKind(deref(cause))
	return inc, err
}

// List returns a page of the user's incidents, newest first, and the total
// matching the filter. A nil filter field matches everything; the casts
// ($2::uuid IS NULL) let one statement serve every filter combination.
func (r *Incidents) List(ctx context.Context, userID uuid.UUID, f domain.IncidentFilter, limit, offset int) ([]domain.Incident, int, error) {
	const where = `
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		WHERE m.user_id = $1
		  AND ($2::uuid IS NULL OR i.monitor_id = $2)
		  AND ($3::boolean IS NULL OR (i.resolved_at IS NULL) = $3)`

	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*)`+where, userID, f.MonitorID, f.Open).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count incidents: %w", err)
	}

	rows, err := r.db.Query(ctx, `SELECT `+incidentColumns+`, m.name`+where+`
		ORDER BY i.started_at DESC, i.id DESC
		LIMIT $4 OFFSET $5`,
		userID, f.MonitorID, f.Open, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list incidents: %w", err)
	}
	incidents, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Incident, error) {
		return scanIncidentWithName(row)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list incidents: %w", err)
	}
	return incidents, total, nil
}

// Get returns one of the user's incidents or domain.ErrNotFound.
func (r *Incidents) Get(ctx context.Context, userID, id uuid.UUID) (domain.Incident, error) {
	inc, err := scanIncidentWithName(r.db.QueryRow(ctx, `
		SELECT `+incidentColumns+`, m.name
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		WHERE i.id = $1 AND m.user_id = $2`,
		id, userID))
	if err != nil {
		return domain.Incident{}, notFound("get incident", err)
	}
	return inc, nil
}
