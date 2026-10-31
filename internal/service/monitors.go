package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

// MonitorRepository stores monitors. Every method is scoped to the owner;
// another user's monitor is domain.ErrNotFound.
type MonitorRepository interface {
	// Create returns domain.ErrLimitExceeded if the user has limit monitors.
	Create(ctx context.Context, userID uuid.UUID, s domain.MonitorSettings, limit int) (domain.Monitor, error)
	Get(ctx context.Context, userID, id uuid.UUID) (domain.Monitor, error)
	List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.Monitor, int, error)
	// Update runs fn on the locked monitor and saves it; an error from fn
	// aborts the update.
	Update(ctx context.Context, userID, id uuid.UUID, fn func(*domain.Monitor) error) (domain.Monitor, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

// URLPolicy decides whether a URL may be monitored (checker.TargetPolicy).
// It returns an error whose text is shown to the client.
type URLPolicy interface {
	ValidateURL(raw string) error
}

// MonitorPatch is the input of create and update. On create, absent fields
// take their defaults; on update, they keep their current values.
type MonitorPatch struct {
	Name               Optional[string] `json:"name"`
	URL                Optional[string] `json:"url"`
	Method             Optional[string] `json:"method"`
	IntervalSeconds    Optional[int]    `json:"interval_seconds"`
	TimeoutSeconds     Optional[int]    `json:"timeout_seconds"`
	ExpectedStatus     Optional[int]    `json:"expected_status"`
	Keyword            Optional[string] `json:"keyword"`
	FollowRedirects    Optional[bool]   `json:"follow_redirects"`
	FailureThreshold   Optional[int]    `json:"failure_threshold"`
	CertExpiryWarnDays Optional[int]    `json:"cert_expiry_warn_days"`
	IsActive           Optional[bool]   `json:"is_active"`
}

// MonitorPage is one page of a list.
type MonitorPage struct {
	Items []domain.Monitor
	Total int
}

// Monitors manages the monitors of a user.
type Monitors struct {
	repo       MonitorRepository
	urls       URLPolicy
	clock      clock.Clock
	validate   inputValidator
	maxPerUser int
}

// NewMonitors returns the Monitors service. maxPerUser is
// MAX_MONITORS_PER_USER.
func NewMonitors(repo MonitorRepository, urls URLPolicy, clk clock.Clock, maxPerUser int) *Monitors {
	return &Monitors{
		repo:       repo,
		urls:       urls,
		clock:      clk,
		validate:   newInputValidator(),
		maxPerUser: maxPerUser,
	}
}

// Create adds a monitor for the user. Errors: *domain.ValidationError,
// domain.ErrLimitExceeded.
func (s *Monitors) Create(ctx context.Context, userID uuid.UUID, p MonitorPatch) (domain.Monitor, error) {
	settings := domain.DefaultMonitorSettings()
	if err := s.applyPatch(&settings, p, true); err != nil {
		return domain.Monitor{}, err
	}
	m, err := s.repo.Create(ctx, userID, settings, s.maxPerUser)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("create monitor: %w", err)
	}
	return m, nil
}

// Get returns one of the user's monitors or domain.ErrNotFound.
func (s *Monitors) Get(ctx context.Context, userID, id uuid.UUID) (domain.Monitor, error) {
	m, err := s.repo.Get(ctx, userID, id)
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("get monitor: %w", err)
	}
	return m, nil
}

// List returns a page of the user's monitors, newest first.
func (s *Monitors) List(ctx context.Context, userID uuid.UUID, limit, offset int) (MonitorPage, error) {
	items, total, err := s.repo.List(ctx, userID, limit, offset)
	if err != nil {
		return MonitorPage{}, fmt.Errorf("list monitors: %w", err)
	}
	return MonitorPage{Items: items, Total: total}, nil
}

// Update changes the fields present in p. Errors: *domain.ValidationError,
// domain.ErrNotFound.
func (s *Monitors) Update(ctx context.Context, userID, id uuid.UUID, p MonitorPatch) (domain.Monitor, error) {
	m, err := s.repo.Update(ctx, userID, id, func(m *domain.Monitor) error {
		before := m.MonitorSettings
		// The URL is re-validated only if it changes: a monitor created
		// under an older, looser policy can still be renamed or paused.
		if err := s.applyPatch(&m.MonitorSettings, p, p.URL.Set); err != nil {
			return err
		}
		m.NextCheckAt = s.reschedule(before, m.MonitorSettings, m.NextCheckAt)
		return nil
	})
	if err != nil {
		return domain.Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	return m, nil
}

// Delete removes one of the user's monitors.
func (s *Monitors) Delete(ctx context.Context, userID, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}
	return nil
}

// reschedule picks the next check time after an update, so a change takes
// effect without waiting out the old interval:
//   - a resumed monitor or one with a new URL is checked at once;
//   - a shortened interval moves the next check no later than now+interval.
func (s *Monitors) reschedule(before, after domain.MonitorSettings, next time.Time) time.Time {
	if !after.IsActive {
		return next
	}
	now := s.clock.Now()
	if !before.IsActive || before.URL != after.URL {
		return now
	}
	if limit := now.Add(time.Duration(after.IntervalSeconds) * time.Second); next.After(limit) {
		return limit
	}
	return next
}

// applyPatch writes the fields present in p into dst, normalizes and
// validates the result. All problems are reported together, sorted by field.
func (s *Monitors) applyPatch(dst *domain.MonitorSettings, p MonitorPatch, checkURL bool) error {
	var details []domain.FieldError
	notNull := func(field string, set, null bool) bool {
		if set && null {
			details = append(details, domain.FieldError{Field: field, Issue: "must not be null"})
			return false
		}
		return set
	}

	if notNull("name", p.Name.Set, p.Name.Null) {
		dst.Name = strings.TrimSpace(p.Name.Value)
	}
	if notNull("url", p.URL.Set, p.URL.Null) {
		dst.URL = strings.TrimSpace(p.URL.Value)
	}
	if notNull("method", p.Method.Set, p.Method.Null) {
		dst.Method = strings.ToUpper(p.Method.Value)
	}
	if notNull("interval_seconds", p.IntervalSeconds.Set, p.IntervalSeconds.Null) {
		dst.IntervalSeconds = p.IntervalSeconds.Value
	}
	if notNull("timeout_seconds", p.TimeoutSeconds.Set, p.TimeoutSeconds.Null) {
		dst.TimeoutSeconds = p.TimeoutSeconds.Value
	}
	if p.ExpectedStatus.Set {
		dst.ExpectedStatus = nullable(p.ExpectedStatus)
	}
	if p.Keyword.Set {
		dst.Keyword = nullable(p.Keyword)
	}
	if notNull("follow_redirects", p.FollowRedirects.Set, p.FollowRedirects.Null) {
		dst.FollowRedirects = p.FollowRedirects.Value
	}
	if notNull("failure_threshold", p.FailureThreshold.Set, p.FailureThreshold.Null) {
		dst.FailureThreshold = p.FailureThreshold.Value
	}
	if notNull("cert_expiry_warn_days", p.CertExpiryWarnDays.Set, p.CertExpiryWarnDays.Null) {
		dst.CertExpiryWarnDays = p.CertExpiryWarnDays.Value
	}
	if notNull("is_active", p.IsActive.Set, p.IsActive.Null) {
		dst.IsActive = p.IsActive.Value
	}

	if err := s.validate.check(dst); err != nil {
		verr, ok := errors.AsType[*domain.ValidationError](err)
		if !ok {
			return err
		}
		for _, d := range verr.Details {
			// A field already reported as null would also fail its rules
			// ("is required"); one message per field is enough.
			if !slices.ContainsFunc(details, func(x domain.FieldError) bool { return x.Field == d.Field }) {
				details = append(details, d)
			}
		}
	}
	if dst.Method == domain.MethodHead && dst.Keyword != nil {
		// A HEAD response has no body, so the keyword could never match.
		details = append(details, domain.FieldError{Field: "keyword", Issue: "requires method GET"})
	}
	urlFailed := slices.ContainsFunc(details, func(d domain.FieldError) bool { return d.Field == "url" })
	if checkURL && !urlFailed {
		if err := s.urls.ValidateURL(dst.URL); err != nil {
			details = append(details, domain.FieldError{Field: "url", Issue: err.Error()})
		}
	}

	if len(details) == 0 {
		return nil
	}
	slices.SortStableFunc(details, func(a, b domain.FieldError) int { return cmp.Compare(a.Field, b.Field) })
	return &domain.ValidationError{Details: details}
}

// nullable turns a present Optional into a pointer: nil for null.
func nullable[T any](o Optional[T]) *T {
	if o.Null {
		return nil
	}
	v := o.Value
	return &v
}
