package handler

import (
	"net/http"
	"strconv"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// Pagination limits (PROMPT.md section 9).
const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

// page is a ?limit=&offset= window.
type page struct {
	Limit  int
	Offset int
}

// listResponse is the envelope of every list endpoint: one page of items
// and the total number of items, so a client can render "1-50 of 123".
type listResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// parsePage reads limit (1-200, default 50) and offset (>= 0, default 0).
// Out-of-range values are errors rather than silently clamped, so a client
// asking for 500 items learns it gets at most 200.
func parsePage(r *http.Request) (page, error) {
	p := page{Limit: defaultPageLimit}
	var details []domain.FieldError
	q := r.URL.Query()

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			details = append(details, domain.FieldError{Field: "limit", Issue: "must be an integer"})
		case n < 1 || n > maxPageLimit:
			details = append(details, domain.FieldError{Field: "limit", Issue: "must be between 1 and 200"})
		default:
			p.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			details = append(details, domain.FieldError{Field: "offset", Issue: "must be an integer"})
		case n < 0:
			details = append(details, domain.FieldError{Field: "offset", Issue: "must not be negative"})
		default:
			p.Offset = n
		}
	}

	if len(details) > 0 {
		return page{}, &domain.ValidationError{Details: details}
	}
	return p, nil
}
