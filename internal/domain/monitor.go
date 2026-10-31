package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrLimitExceeded: the user already has the maximum number of monitors.
var ErrLimitExceeded = errors.New("limit exceeded")

// MonitorStatus is the state derived from recent checks.
type MonitorStatus string

// Monitor statuses.
const (
	StatusUnknown MonitorStatus = "unknown" // not checked yet
	StatusUp      MonitorStatus = "up"
	StatusDown    MonitorStatus = "down"
)

// HTTP methods a monitor may use.
const (
	MethodGet  = "GET"
	MethodHead = "HEAD"
)

// MonitorSettings is the user-editable part of a monitor. The struct tags
// are the validation rules (go-playground/validator) and the field names
// reported in validation errors; they match the API and the CHECK
// constraints of the monitors table.
type MonitorSettings struct {
	Name            string  `json:"name"             validate:"required,max=100"`
	URL             string  `json:"url"              validate:"required"`
	Method          string  `json:"method"           validate:"oneof=GET HEAD"`
	IntervalSeconds int     `json:"interval_seconds" validate:"min=30,max=86400"`
	TimeoutSeconds  int     `json:"timeout_seconds"  validate:"min=1,max=30"`
	ExpectedStatus  *int    `json:"expected_status"  validate:"omitempty,min=100,max=599"` // nil: any 2xx
	Keyword         *string `json:"keyword"          validate:"omitempty,min=1,max=1000"`  // nil: body not checked
	FollowRedirects bool    `json:"follow_redirects"`
	// FailureThreshold is how many failed checks in a row open an incident.
	FailureThreshold   int  `json:"failure_threshold"     validate:"min=1,max=10"`
	CertExpiryWarnDays int  `json:"cert_expiry_warn_days" validate:"min=1,max=90"`
	IsActive           bool `json:"is_active"`
}

// DefaultMonitorSettings are the values of fields a client leaves out when
// creating a monitor. They match the column defaults of the monitors table.
func DefaultMonitorSettings() MonitorSettings {
	return MonitorSettings{
		Method:             MethodGet,
		IntervalSeconds:    60,
		TimeoutSeconds:     10,
		FollowRedirects:    true,
		FailureThreshold:   3,
		CertExpiryWarnDays: 14,
		IsActive:           true,
	}
}

// Monitor is a URL checked periodically on behalf of a user.
type Monitor struct {
	ID     uuid.UUID
	UserID uuid.UUID
	MonitorSettings

	Status              MonitorStatus
	ConsecutiveFailures int
	TLSNotAfter         *time.Time // expiry of the last seen certificate
	NextCheckAt         time.Time
	LastCheckedAt       *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
