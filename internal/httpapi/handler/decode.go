package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// decodeJSON reads one JSON object from the request body into dst. Malformed
// JSON, unknown fields, trailing data and oversized bodies are reported as a
// *domain.ValidationError on the "body" field, so the client gets a 400 that
// says what is wrong.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	// A typo in a field name ("pasword") should fail loudly rather than be
	// silently ignored.
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return bodyError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.NewValidationError("body", "must contain a single JSON object")
	}
	return nil
}

func bodyError(err error) error {
	if tooLarge, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return domain.NewValidationError("body", fmt.Sprintf("must be at most %d bytes", tooLarge.Limit))
	}
	if errors.Is(err, io.EOF) {
		return domain.NewValidationError("body", "is required")
	}
	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok && typeErr.Field != "" {
		return domain.NewValidationError(typeErr.Field, "must be a "+typeErr.Type.Kind().String())
	}
	// encoding/json has no error type for this case, only the message.
	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return domain.NewValidationError(strings.Trim(field, `"`), "is not a known field")
	}
	// Syntax errors. The message describes only what the client sent.
	return domain.NewValidationError("body", "is not valid JSON: "+err.Error())
}
