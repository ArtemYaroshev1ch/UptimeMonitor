// Package postgres implements the repositories on PostgreSQL with plain SQL.
// Every query is parameterized; values are never spliced into SQL text.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// uniqueViolation is the PostgreSQL error code for a unique constraint violation.
const uniqueViolation = "23505"

// DB is the subset of pgxpool.Pool that repositories use. pgx.Tx implements
// it too, so a repository can run inside a transaction.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Users stores user accounts.
type Users struct {
	db DB
}

// NewUsers returns a Users repository.
func NewUsers(db DB) *Users {
	return &Users{db: db}
}

const userColumns = `id, email, password_hash, created_at`

func scanUser(row pgx.Row) (domain.User, error) {
	var u domain.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

// Create inserts a user. A taken email returns domain.ErrConflict.
func (r *Users) Create(ctx context.Context, email, passwordHash string) (domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING `+userColumns,
		email, passwordHash))
	if err != nil {
		// The unique index is the source of truth for "email taken": a
		// "SELECT first" check would race with a concurrent registration.
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == uniqueViolation {
			return domain.User{}, fmt.Errorf("create user: email taken: %w", domain.ErrConflict)
		}
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// GetByEmail returns the user with the given email or domain.ErrNotFound.
func (r *Users) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email))
	if err != nil {
		return domain.User{}, notFound("get user by email", err)
	}
	return u, nil
}

// GetByID returns the user with the given ID or domain.ErrNotFound.
func (r *Users) GetByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	u, err := scanUser(r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if err != nil {
		return domain.User{}, notFound("get user by id", err)
	}
	return u, nil
}

// notFound wraps err, translating "no rows" into domain.ErrNotFound.
func notFound(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", op, domain.ErrNotFound)
	}
	return fmt.Errorf("%s: %w", op, err)
}
