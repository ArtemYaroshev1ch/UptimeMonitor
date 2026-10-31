package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/logger"
)

// TokenVerifier checks an access token and returns its user ID
// (auth.Tokens).
type TokenVerifier interface {
	Verify(token string) (uuid.UUID, error)
}

type userIDKey struct{}

// Authenticate requires a valid "Authorization: Bearer <token>" header. It
// stores the user ID in the context (see UserID) and answers 401 otherwise.
// Every failure gets the same response, so a client cannot tell an expired
// token from a forged one.
func Authenticate(tokens TokenVerifier, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				response.Error(w, r, log, fmt.Errorf("no bearer token: %w", domain.ErrUnauthorized))
				return
			}
			userID, err := tokens.Verify(token)
			if err != nil {
				// Debug only: bad tokens are routine (expiry) and the token
				// itself is never logged.
				log.DebugContext(r.Context(), "token rejected", slog.Any("error", err))
				response.Error(w, r, log, fmt.Errorf("verify token: %w", domain.ErrUnauthorized))
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey{}, userID)
			ctx = logger.WithAttrs(ctx, slog.String("user_id", userID.String()))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserID returns the authenticated user set by Authenticate. ok is false on
// routes without Authenticate.
func UserID(ctx context.Context) (id uuid.UUID, ok bool) {
	id, ok = ctx.Value(userIDKey{}).(uuid.UUID)
	return id, ok
}

// bearerToken extracts the token from the Authorization header. The scheme
// name is case-insensitive (RFC 9110).
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
