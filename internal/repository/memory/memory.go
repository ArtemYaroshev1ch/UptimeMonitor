// Package memory implements the repositories in memory, for tests of the
// layers above storage (services, HTTP handlers). It mirrors the semantics
// of the PostgreSQL repositories: ownership scoping, ErrNotFound,
// ErrConflict on duplicate emails and the per-user monitor limit. Those
// semantics are verified against PostgreSQL by the integration tests of
// repository/postgres.
package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// failable makes every call of a repository return an injected error, to
// simulate a database outage.
type failable struct {
	mu  sync.Mutex
	err error
}

// FailWith makes all subsequent calls return err (nil restores normal work).
func (f *failable) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Users is an in-memory user repository.
type Users struct {
	failable
	users []domain.User
	now   func() time.Time
}

// NewUsers returns an empty Users. now stamps CreatedAt.
func NewUsers(now func() time.Time) *Users {
	return &Users{now: now}
}

// Create stores a user; a taken email returns domain.ErrConflict.
func (r *Users) Create(_ context.Context, email, passwordHash string) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.User{}, r.err
	}
	if slices.ContainsFunc(r.users, func(u domain.User) bool { return u.Email == email }) {
		return domain.User{}, domain.ErrConflict
	}
	u := domain.User{ID: uuid.New(), Email: email, PasswordHash: passwordHash, CreatedAt: r.now()}
	r.users = append(r.users, u)
	return u, nil
}

// GetByEmail returns the user or domain.ErrNotFound.
func (r *Users) GetByEmail(_ context.Context, email string) (domain.User, error) {
	return r.find(func(u domain.User) bool { return u.Email == email })
}

// GetByID returns the user or domain.ErrNotFound.
func (r *Users) GetByID(_ context.Context, id uuid.UUID) (domain.User, error) {
	return r.find(func(u domain.User) bool { return u.ID == id })
}

// Len returns the number of stored users.
func (r *Users) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.users)
}

func (r *Users) find(match func(domain.User) bool) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.User{}, r.err
	}
	if i := slices.IndexFunc(r.users, match); i >= 0 {
		return r.users[i], nil
	}
	return domain.User{}, domain.ErrNotFound
}

// Monitors is an in-memory monitor repository. Monitors are kept in
// creation order; List returns them newest first, like PostgreSQL.
type Monitors struct {
	failable
	monitors []domain.Monitor
	now      func() time.Time
}

// NewMonitors returns an empty Monitors. now stamps CreatedAt, UpdatedAt and
// the initial NextCheckAt.
func NewMonitors(now func() time.Time) *Monitors {
	return &Monitors{now: now}
}

// Create stores a monitor unless the user has limit monitors already.
func (r *Monitors) Create(_ context.Context, userID uuid.UUID, s domain.MonitorSettings, limit int) (domain.Monitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.Monitor{}, r.err
	}
	count := 0
	for _, m := range r.monitors {
		if m.UserID == userID {
			count++
		}
	}
	if count >= limit {
		return domain.Monitor{}, domain.ErrLimitExceeded
	}
	now := r.now()
	m := domain.Monitor{
		ID: uuid.New(), UserID: userID, MonitorSettings: s, Status: domain.StatusUnknown,
		NextCheckAt: now, CreatedAt: now, UpdatedAt: now,
	}
	r.monitors = append(r.monitors, m)
	return m, nil
}

// Get returns the user's monitor or domain.ErrNotFound.
func (r *Monitors) Get(_ context.Context, userID, id uuid.UUID) (domain.Monitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.Monitor{}, r.err
	}
	i := r.index(userID, id)
	if i < 0 {
		return domain.Monitor{}, domain.ErrNotFound
	}
	return r.monitors[i], nil
}

// List returns a page of the user's monitors, newest first, and their total.
func (r *Monitors) List(_ context.Context, userID uuid.UUID, limit, offset int) ([]domain.Monitor, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, 0, r.err
	}
	var own []domain.Monitor
	for i := len(r.monitors) - 1; i >= 0; i-- {
		if r.monitors[i].UserID == userID {
			own = append(own, r.monitors[i])
		}
	}
	total := len(own)
	return own[min(offset, total):min(offset+limit, total)], total, nil
}

// Update applies fn to the user's monitor and stores the result unless fn
// fails.
func (r *Monitors) Update(_ context.Context, userID, id uuid.UUID, fn func(*domain.Monitor) error) (domain.Monitor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return domain.Monitor{}, r.err
	}
	i := r.index(userID, id)
	if i < 0 {
		return domain.Monitor{}, domain.ErrNotFound
	}
	m := r.monitors[i]
	if err := fn(&m); err != nil {
		return domain.Monitor{}, err
	}
	m.UpdatedAt = r.now()
	r.monitors[i] = m
	return m, nil
}

// Delete removes the user's monitor or returns domain.ErrNotFound.
func (r *Monitors) Delete(_ context.Context, userID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	i := r.index(userID, id)
	if i < 0 {
		return domain.ErrNotFound
	}
	r.monitors = slices.Delete(r.monitors, i, i+1)
	return nil
}

// Put stores m as is, bypassing validation and the limit: test setup for
// states the API cannot produce (e.g. a URL the current policy rejects).
func (r *Monitors) Put(m domain.Monitor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.IndexFunc(r.monitors, func(x domain.Monitor) bool { return x.ID == m.ID }); i >= 0 {
		r.monitors[i] = m
		return
	}
	r.monitors = append(r.monitors, m)
}

// Len returns the number of stored monitors of all users.
func (r *Monitors) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.monitors)
}

func (r *Monitors) index(userID, id uuid.UUID) int {
	return slices.IndexFunc(r.monitors, func(m domain.Monitor) bool { return m.ID == id && m.UserID == userID })
}
