package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// decodeJSON reads one JSON object from the request body into dst (a pointer
// to a struct). Malformed JSON, unknown fields, wrong types, trailing data
// and oversized bodies are reported as a *domain.ValidationError naming the
// field (or "body"), so the client gets a 400 that says what is wrong.
func decodeJSON(r *http.Request, dst any) error {
	// The body is bounded by middleware.BodyLimit.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return bodyError(err, nil, dst)
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	// A typo in a field name ("pasword") should fail loudly rather than be
	// silently ignored.
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return bodyError(err, body, dst)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.NewValidationError("body", "must contain a single JSON object")
	}
	return nil
}

func bodyError(err error, body []byte, dst any) error {
	if tooLarge, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return domain.NewValidationError("body", fmt.Sprintf("must be at most %d bytes", tooLarge.Limit))
	}
	if errors.Is(err, io.EOF) {
		return domain.NewValidationError("body", "is required")
	}
	if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		field := typeErr.Field
		if field == "" {
			// Errors returned by a field's own UnmarshalJSON (such as
			// service.Optional) carry no field name.
			field = failingField(body, dst)
		}
		if field != "" {
			return domain.NewValidationError(field, "must be "+kindName(typeErr.Type))
		}
	}
	// encoding/json has no error type for this case, only the message.
	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return domain.NewValidationError(strings.Trim(field, `"`), "is not a known field")
	}
	// Syntax errors. The message describes only what the client sent.
	return domain.NewValidationError("body", "is not valid JSON: "+err.Error())
}

// failingField returns the JSON name of the first top-level field of dst
// whose value in body does not decode, or "".
func failingField(body []byte, dst any) string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	t := reflect.TypeOf(dst)
	if t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return ""
	}
	t = t.Elem()
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		value, ok := raw[name]
		if !ok {
			continue
		}
		if json.Unmarshal(value, reflect.New(f.Type).Interface()) != nil {
			return name
		}
	}
	return ""
}

// kindName describes a Go type the way an API client thinks of it.
func kindName(t reflect.Type) string {
	if t == nil {
		return "of a different type"
	}
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "a boolean"
	case reflect.Slice, reflect.Array:
		return "an array"
	default:
		return "an object"
	}
}
