package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/middleware"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/httpapi/response"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/service"
)

// AuthService is the part of service.Auth the handlers use.
type AuthService interface {
	Register(ctx context.Context, c service.Credentials) (domain.User, error)
	Login(ctx context.Context, c service.Credentials) (service.Session, error)
	Me(ctx context.Context, userID uuid.UUID) (domain.User, error)
}

// Auth serves registration, login and the current user.
type Auth struct {
	svc AuthService
	log *slog.Logger
}

// NewAuth returns the auth handlers.
func NewAuth(svc AuthService, log *slog.Logger) Auth {
	return Auth{svc: svc, log: log}
}

type userResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func newUserResponse(u domain.User) userResponse {
	return userResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt.UTC()}
}

type tokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Register handles POST /api/v1/auth/register.
func (h Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req service.Credentials
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	u, err := h.svc.Register(r.Context(), req)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusCreated, newUserResponse(u))
}

// Login handles POST /api/v1/auth/login.
func (h Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req service.Credentials
	if err := decodeJSON(r, &req); err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	s, err := h.svc.Login(r.Context(), req)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	// A token is a credential: no cache may keep a copy (RFC 6749, 5.1).
	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, tokenResponse{
		AccessToken: s.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   s.ExpiresAt.UTC(),
	})
}

// Me handles GET /api/v1/me. It must run behind middleware.Authenticate.
func (h Auth) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserID(r.Context())
	if !ok {
		// Only a routing mistake gets here: the route lacks Authenticate.
		response.Error(w, r, h.log, fmt.Errorf("me: route is not authenticated"))
		return
	}
	u, err := h.svc.Me(r.Context(), userID)
	if err != nil {
		response.Error(w, r, h.log, err)
		return
	}
	response.JSON(w, http.StatusOK, newUserResponse(u))
}
