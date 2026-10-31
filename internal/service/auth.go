package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/auth"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
)

// UserRepository stores users.
type UserRepository interface {
	// Create returns domain.ErrConflict if the email is taken.
	Create(ctx context.Context, email, passwordHash string) (domain.User, error)
	// GetByEmail and GetByID return domain.ErrNotFound for unknown users.
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (domain.User, error)
}

// PasswordHasher hashes and checks passwords (auth.Passwords).
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare returns auth.ErrPasswordMismatch for a wrong password.
	Compare(hash, password string) error
}

// TokenIssuer issues access tokens (auth.Tokens).
type TokenIssuer interface {
	Issue(userID uuid.UUID) (token string, expiresAt time.Time, err error)
}

// Credentials are an email and password, as sent to register or log in.
type Credentials struct {
	Email    string `json:"email"    validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,min=8"`
}

// Session is the result of a successful login.
type Session struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Auth registers users, logs them in and resolves token owners.
type Auth struct {
	users     UserRepository
	passwords PasswordHasher
	tokens    TokenIssuer
	validate  inputValidator

	// dummyHash is compared against when the email is unknown, so a login
	// for a missing user takes as long as one with a wrong password. Without
	// it, the response time would tell an attacker which emails exist.
	dummyHash string
}

// NewAuth returns the Auth service.
func NewAuth(users UserRepository, passwords PasswordHasher, tokens TokenIssuer) (*Auth, error) {
	dummy, err := passwords.Hash("dummy password for timing equalization")
	if err != nil {
		return nil, fmt.Errorf("auth service: %w", err)
	}
	return &Auth{
		users:     users,
		passwords: passwords,
		tokens:    tokens,
		validate:  newInputValidator(),
		dummyHash: dummy,
	}, nil
}

// normalizeEmail makes "Alice@Example.com " and "alice@example.com" the same
// account. Strictly, the local part is case-sensitive, but no real mail
// provider treats it so, and users do not remember how they typed it.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Register creates an account. It returns a *domain.ValidationError for bad
// input and domain.ErrConflict if the email is taken.
func (s *Auth) Register(ctx context.Context, c Credentials) (domain.User, error) {
	c.Email = normalizeEmail(c.Email)
	if err := s.validate.check(c); err != nil {
		return domain.User{}, err
	}
	// bcrypt ignores everything after 72 bytes; accepting a longer password
	// would let a different password with the same prefix log in.
	if len(c.Password) > auth.MaxPasswordBytes {
		return domain.User{}, domain.NewValidationError("password",
			fmt.Sprintf("must be at most %d bytes", auth.MaxPasswordBytes))
	}

	hash, err := s.passwords.Hash(c.Password)
	if err != nil {
		return domain.User{}, fmt.Errorf("register: %w", err)
	}
	u, err := s.users.Create(ctx, c.Email, hash)
	if err != nil {
		return domain.User{}, fmt.Errorf("register: %w", err)
	}
	return u, nil
}

// Login checks the credentials and issues an access token. Unknown email and
// wrong password both return domain.ErrUnauthorized, indistinguishably.
func (s *Auth) Login(ctx context.Context, c Credentials) (Session, error) {
	c.Email = normalizeEmail(c.Email)
	// Only presence is checked: password rules may change, and old accounts
	// must still be able to log in.
	if c.Email == "" || c.Password == "" {
		verr := &domain.ValidationError{}
		if c.Email == "" {
			verr.Details = append(verr.Details, domain.FieldError{Field: "email", Issue: "is required"})
		}
		if c.Password == "" {
			verr.Details = append(verr.Details, domain.FieldError{Field: "password", Issue: "is required"})
		}
		return Session{}, verr
	}

	u, err := s.users.GetByEmail(ctx, c.Email)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		// Burn the same CPU time as a real comparison; the result is ignored.
		_ = s.passwords.Compare(s.dummyHash, c.Password)
		return Session{}, fmt.Errorf("login: invalid credentials: %w", domain.ErrUnauthorized)
	case err != nil:
		return Session{}, fmt.Errorf("login: %w", err)
	}

	if err := s.passwords.Compare(u.PasswordHash, c.Password); err != nil {
		if errors.Is(err, auth.ErrPasswordMismatch) {
			return Session{}, fmt.Errorf("login: invalid credentials: %w", domain.ErrUnauthorized)
		}
		return Session{}, fmt.Errorf("login: %w", err)
	}

	token, expiresAt, err := s.tokens.Issue(u.ID)
	if err != nil {
		return Session{}, fmt.Errorf("login: %w", err)
	}
	return Session{AccessToken: token, ExpiresAt: expiresAt}, nil
}

// Me returns the user a verified token belongs to. A token of a user that no
// longer exists is treated as invalid: domain.ErrUnauthorized.
func (s *Auth) Me(ctx context.Context, userID uuid.UUID) (domain.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return domain.User{}, fmt.Errorf("me: user gone: %w", domain.ErrUnauthorized)
	case err != nil:
		return domain.User{}, fmt.Errorf("me: %w", err)
	}
	return u, nil
}
