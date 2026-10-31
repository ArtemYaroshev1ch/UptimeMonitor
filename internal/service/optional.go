package service

import (
	"bytes"
	"encoding/json"
)

// Optional is one field of a partial update (PATCH). A plain pointer cannot
// tell `{}` (leave the field alone) from `{"keyword": null}` (clear it);
// Optional can:
//
//	absent:          Set == false
//	"field": null    Set == true, Null == true
//	"field": value   Set == true, Value == value
type Optional[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// Some returns an Optional holding v. Handy in tests and callers that build
// updates in code.
func Some[T any](v T) Optional[T] {
	return Optional[T]{Set: true, Value: v}
}

// UnmarshalJSON implements json.Unmarshaler. encoding/json calls it only for
// fields present in the input, null included, so Set records presence.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Value)
}
