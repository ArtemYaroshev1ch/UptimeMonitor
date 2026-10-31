package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// TxDB is a DB that can start transactions (pgxpool.Pool).
type TxDB interface {
	DB
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Monitors stores monitors. Every method takes the owner's ID and filters by
// it, so one user can never read or change another user's monitor: a foreign
// monitor is indistinguishable from a missing one (domain.ErrNotFound).
type Monitors struct {
	db TxDB
}

// NewMonitors returns a Monitors repository.
func NewMonitors(db TxDB) *Monitors {
	return &Monitors{db: db}
}

const monitorColumns = `id, user_id, name, url, method, interval_seconds, timeout_seconds,
	expected_status, keyword, follow_redirects, failure_threshold, cert_expiry_warn_days,
	is_active, status, consecutive_failures, tls_not_after, next_check_at, last_checked_at,
	created_at, updated_at`

func scanMonitor(row pgx.Row) (domain.Monitor, error) {
	var m domain.Monitor
	err := row.Scan(&m.ID, &m.UserID, &m.Name, &m.URL, &m.Method, &m.IntervalSeconds, &m.TimeoutSeconds,
		&m.ExpectedStatus, &m.Keyword, &m.FollowRedirects, &m.FailureThreshold, &m.CertExpiryWarnDays,
		&m.IsActive, &m.Status, &m.ConsecutiveFailures, &m.TLSNotAfter, &m.NextCheckAt, &m.LastCheckedAt,
		&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

// Create inserts a monitor unless the user already has limit monitors, in
// which case it returns domain.ErrLimitExceeded.
func (r *Monitors) Create(ctx context.Context, userID uuid.UUID, s domain.MonitorSettings, limit int) (domain.Monitor, error) {
	var m domain.Monitor
	// pgx.BeginFunc commits if the function returns nil and rolls back
	// otherwise, also on panic.
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		// Lock the owner's row first. Without it, two concurrent requests
		// of a user with limit-1 monitors would both count limit-1 and
		// both insert. Requests of the same user now queue up here; other
		// users are not affected.
		var locked uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&locked)
		if err != nil {
			return notFound("lock user", err)
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM monitors WHERE user_id = $1`, userID).Scan(&count); err != nil {
			return fmt.Errorf("count monitors: %w", err)
		}
		if count >= limit {
			return fmt.Errorf("%d monitors: %w", count, domain.ErrLimitExceeded)
		}

		m, err = scanMonitor(tx.QueryRow(ctx, `
			INSERT INTO monitors (user_id, name, url, method, interval_seconds, timeout_seconds,
				expected_status, keyword, follow_redirects, failure_threshold, cert_expiry_warn_days, is_active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			RETURNING `+monitorColumns,
			userID, s.Name, s.URL, s.Method, s.IntervalSeconds, s.TimeoutSeconds,
			s.ExpectedStatus, s.Keyword, s.FollowRedirects, s.FailureThreshold, s.CertExpiryWarnDays, s.IsActive))
		if err != nil {
			return fmt.Errorf("insert monitor: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	return m, nil
}

// Get returns one monitor of the user or domain.ErrNotFound.
func (r *Monitors) Get(ctx context.Context, userID, id uuid.UUID) (domain.Monitor, error) {
	m, err := scanMonitor(r.db.QueryRow(ctx,
		`SELECT `+monitorColumns+` FROM monitors WHERE id = $1 AND user_id = $2`, id, userID))
	if err != nil {
		return domain.Monitor{}, notFound("get monitor", err)
	}
	return m, nil
}

// List returns a page of the user's monitors, newest first, and the total
// number of the user's monitors.
func (r *Monitors) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Monitor, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT count(*) FROM monitors WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count monitors: %w", err)
	}

	// id breaks ties between monitors created in the same microsecond, so
	// pages never overlap or skip rows.
	rows, err := r.db.Query(ctx, `
		SELECT `+monitorColumns+` FROM monitors
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}
	monitors, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Monitor, error) {
		return scanMonitor(row)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list monitors: %w", err)
	}
	return monitors, total, nil
}

// Update loads the user's monitor, lets fn change it and saves the result,
// all in one transaction. The row stays locked in between, so two concurrent
// updates are applied one after the other and neither is lost. fn may change
// MonitorSettings and NextCheckAt; an error from fn aborts the update and is
// returned as is.
func (r *Monitors) Update(ctx context.Context, userID, id uuid.UUID, fn func(*domain.Monitor) error) (domain.Monitor, error) {
	var m domain.Monitor
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		var err error
		m, err = scanMonitor(tx.QueryRow(ctx,
			`SELECT `+monitorColumns+` FROM monitors WHERE id = $1 AND user_id = $2 FOR UPDATE`, id, userID))
		if err != nil {
			return notFound("get monitor", err)
		}

		if err := fn(&m); err != nil {
			return err
		}

		s := m.MonitorSettings
		m, err = scanMonitor(tx.QueryRow(ctx, `
			UPDATE monitors SET
				name = $3, url = $4, method = $5, interval_seconds = $6, timeout_seconds = $7,
				expected_status = $8, keyword = $9, follow_redirects = $10, failure_threshold = $11,
				cert_expiry_warn_days = $12, is_active = $13, next_check_at = $14, updated_at = now()
			WHERE id = $1 AND user_id = $2
			RETURNING `+monitorColumns,
			id, userID, s.Name, s.URL, s.Method, s.IntervalSeconds, s.TimeoutSeconds,
			s.ExpectedStatus, s.Keyword, s.FollowRedirects, s.FailureThreshold,
			s.CertExpiryWarnDays, s.IsActive, m.NextCheckAt))
		if err != nil {
			return fmt.Errorf("save monitor: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	return m, nil
}

// Delete removes the user's monitor or returns domain.ErrNotFound.
func (r *Monitors) Delete(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM monitors WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete monitor: %w", domain.ErrNotFound)
	}
	return nil
}
