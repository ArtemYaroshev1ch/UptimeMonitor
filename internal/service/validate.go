// Package service holds the business logic. It depends on interfaces it
// declares itself (repositories, hashers), never on HTTP or SQL.
package service

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// inputValidator checks struct tags (`validate:"required,email"`) and turns
// failures into a domain.ValidationError whose field names are the JSON names
// the client sent.
type inputValidator struct {
	v *validator.Validate
}

func newInputValidator() inputValidator {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return f.Name
		}
		return name
	})
	return inputValidator{v: v}
}

// check validates s. It returns nil, a *domain.ValidationError, or an
// internal error if s is not a struct (a programming error).
func (iv inputValidator) check(s any) error {
	err := iv.v.Struct(s)
	if err == nil {
		return nil
	}
	fieldErrs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		return fmt.Errorf("validate input: %w", err)
	}
	verr := &domain.ValidationError{}
	for _, fe := range fieldErrs {
		verr.Details = append(verr.Details, domain.FieldError{Field: fe.Field(), Issue: issue(fe)})
	}
	return verr
}

// issue renders a failed rule as a message for the client.
func issue(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	default:
		return "is invalid (" + fe.Tag() + ")"
	}
}
