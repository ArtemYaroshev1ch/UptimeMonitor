package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidationErrorMessage(t *testing.T) {
	t.Parallel()

	err := &ValidationError{Details: []FieldError{
		{Field: "email", Issue: "must be a valid email address"},
		{Field: "password", Issue: "must be at least 8 characters"},
	}}

	assert.Equal(t,
		"validation failed: email: must be a valid email address; password: must be at least 8 characters",
		err.Error())
}
