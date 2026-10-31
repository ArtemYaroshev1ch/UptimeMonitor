// Package domain holds the entities and errors shared by all layers. It
// depends on nothing else in the project.
package domain

import (
	"errors"
	"strings"
)

// Sentinel errors. Lower layers wrap them with context; the HTTP layer maps
// them to status codes in one place (httpapi/response.Error).
var (
	// ErrNotFound: the resource does not exist or belongs to another user.
	ErrNotFound = errors.New("not found")
	// ErrConflict: the request clashes with existing state (duplicate email).
	ErrConflict = errors.New("conflict")
	// ErrUnauthorized: missing or invalid credentials.
	ErrUnauthorized = errors.New("unauthorized")
)

// FieldError describes one invalid input field.
type FieldError struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

// ValidationError is returned when input is malformed. Its details are safe
// to show to the client.
type ValidationError struct {
	Details []FieldError
}

// NewValidationError returns a ValidationError for a single field.
func NewValidationError(field, issue string) *ValidationError {
	return &ValidationError{Details: []FieldError{{Field: field, Issue: issue}}}
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Details))
	for _, d := range e.Details {
		parts = append(parts, d.Field+": "+d.Issue)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
