package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// Error codes of the API error format (PROMPT.md section 9). They are part of
// the public API: clients switch on them, so do not rename them.
const (
	CodeValidationFailed = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeRateLimited      = "rate_limited"
	CodeInternal         = "internal"
)

type errorBody struct {
	Error errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []domain.FieldError `json:"details,omitempty"`
}

// WriteError writes an error in the API format:
//
//	{"error": {"code": "...", "message": "...", "details": [...]}}
func WriteError(w http.ResponseWriter, status int, code, message string, details []domain.FieldError) {
	JSON(w, status, errorBody{Error: errorPayload{Code: code, Message: message, Details: details}})
}

// Error maps err to an HTTP response. This is the only place where domain
// errors become status codes. Unknown errors are 500s: the client gets a
// generic message, the log gets the error together with the request_id
// (from the request context), so the two can be matched.
func Error(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	if verr, ok := errors.AsType[*domain.ValidationError](err); ok {
		WriteError(w, http.StatusBadRequest, CodeValidationFailed, "Invalid request", verr.Details)
		return
	}

	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		// RFC 9110: a 401 must say which authentication scheme to use.
		w.Header().Set("WWW-Authenticate", `Bearer realm="uptime-monitor"`)
		WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "Authentication required", nil)
	case errors.Is(err, domain.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "Resource not found", nil)
	case errors.Is(err, domain.ErrConflict):
		WriteError(w, http.StatusConflict, CodeConflict, "Resource already exists", nil)
	case errors.Is(err, domain.ErrLimitExceeded):
		// A conflict with the current state: deleting something resolves it.
		WriteError(w, http.StatusConflict, CodeConflict, "Limit reached", nil)
	default:
		log.ErrorContext(r.Context(), "request failed", slog.Any("error", err))
		WriteError(w, http.StatusInternalServerError, CodeInternal, "Internal server error", nil)
	}
}
