package service

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/auth"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/domain"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
	"github.com/artemyaroshev1ch/uptime-monitor/internal/repository/memory"
)

// countingHasher counts Compare calls of the real bcrypt hasher.
type countingHasher struct {
	auth.Passwords
	mu       sync.Mutex
	compares int
}

func (h *countingHasher) Compare(hash, password string) error {
	h.mu.Lock()
	h.compares++
	h.mu.Unlock()
	return h.Passwords.Compare(hash, password)
}

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type testEnv struct {
	svc    *Auth
	users  *memory.Users
	hasher *countingHasher
	tokens *auth.Tokens
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	users := memory.NewUsers(time.Now)
	hasher := &countingHasher{Passwords: auth.NewPasswords(bcrypt.MinCost)}
	tokens := auth.NewTokens([]byte("0123456789abcdef0123456789abcdef"), time.Hour, clock.NewFake(testNow))
	svc, err := NewAuth(users, hasher, tokens)
	require.NoError(t, err)
	return testEnv{svc: svc, users: users, hasher: hasher, tokens: tokens}
}

func validationDetails(t *testing.T, err error) []domain.FieldError {
	t.Helper()
	verr, ok := errors.AsType[*domain.ValidationError](err)
	require.True(t, ok, "want *domain.ValidationError, got %v", err)
	return verr.Details
}

func TestRegister(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	u, err := env.svc.Register(t.Context(), Credentials{Email: "  Alice@Example.COM ", Password: "s3cret-password"})

	require.NoError(t, err)
	assert.Equal(t, "alice@example.com", u.Email, "email is normalized")
	assert.NotEqual(t, "s3cret-password", u.PasswordHash, "only the hash is stored")
	require.NoError(t, env.hasher.Passwords.Compare(u.PasswordHash, "s3cret-password"))
}

func TestRegisterDuplicateEmail(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	_, err := env.svc.Register(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})
	require.NoError(t, err)

	// Differs only in case: the same account after normalization.
	_, err = env.svc.Register(t.Context(), Credentials{Email: "ALICE@example.com", Password: "password-2"})

	require.ErrorIs(t, err, domain.ErrConflict)
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		creds Credentials
		want  []domain.FieldError
	}{
		{
			name:  "empty",
			creds: Credentials{},
			want: []domain.FieldError{
				{Field: "email", Issue: "is required"},
				{Field: "password", Issue: "is required"},
			},
		},
		{
			name:  "malformed email",
			creds: Credentials{Email: "not-an-email", Password: "password-1"},
			want:  []domain.FieldError{{Field: "email", Issue: "must be a valid email address"}},
		},
		{
			name:  "email with display name",
			creds: Credentials{Email: "Alice <alice@example.com>", Password: "password-1"},
			want:  []domain.FieldError{{Field: "email", Issue: "must be a valid email address"}},
		},
		{
			name:  "short password",
			creds: Credentials{Email: "alice@example.com", Password: "1234567"},
			want:  []domain.FieldError{{Field: "password", Issue: "must be at least 8 characters"}},
		},
		{
			name:  "password longer than bcrypt accepts",
			creds: Credentials{Email: "alice@example.com", Password: strings.Repeat("x", 73)},
			want:  []domain.FieldError{{Field: "password", Issue: "must be at most 72 bytes"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(t)

			_, err := env.svc.Register(t.Context(), tt.creds)

			assert.Equal(t, tt.want, validationDetails(t, err))
			assert.Zero(t, env.users.Len(), "nothing is stored")
		})
	}
}

func TestRegisterRepositoryFailure(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	dbErr := errors.New("connection reset")
	env.users.FailWith(dbErr)

	_, err := env.svc.Register(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})

	require.ErrorIs(t, err, dbErr)
}

func TestLogin(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	u, err := env.svc.Register(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})
	require.NoError(t, err)

	session, err := env.svc.Login(t.Context(), Credentials{Email: " ALICE@example.com", Password: "password-1"})

	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Hour), session.ExpiresAt)
	userID, err := env.tokens.Verify(session.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, u.ID, userID)
}

func TestLoginRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		creds Credentials
	}{
		{name: "wrong password", creds: Credentials{Email: "alice@example.com", Password: "password-2"}},
		{name: "unknown email", creds: Credentials{Email: "bob@example.com", Password: "password-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(t)
			_, err := env.svc.Register(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})
			require.NoError(t, err)

			_, err = env.svc.Login(t.Context(), tt.creds)

			require.ErrorIs(t, err, domain.ErrUnauthorized)
			// Both cases run exactly one bcrypt comparison, so they take
			// the same time and do not reveal whether the email exists.
			assert.Equal(t, 1, env.hasher.compares)
		})
	}
}

func TestLoginValidation(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	_, err := env.svc.Login(t.Context(), Credentials{Email: "   "})

	assert.Equal(t, []domain.FieldError{
		{Field: "email", Issue: "is required"},
		{Field: "password", Issue: "is required"},
	}, validationDetails(t, err))
}

func TestLoginRepositoryFailure(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	dbErr := errors.New("connection reset")
	env.users.FailWith(dbErr)

	_, err := env.svc.Login(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})

	require.ErrorIs(t, err, dbErr)
	assert.NotErrorIs(t, err, domain.ErrUnauthorized, "an outage is not a wrong password")
}

func TestMe(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	u, err := env.svc.Register(t.Context(), Credentials{Email: "alice@example.com", Password: "password-1"})
	require.NoError(t, err)

	got, err := env.svc.Me(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, u, got)

	_, err = env.svc.Me(t.Context(), uuid.New())
	require.ErrorIs(t, err, domain.ErrUnauthorized, "token of a deleted user")

	dbErr := errors.New("connection reset")
	env.users.FailWith(dbErr)
	_, err = env.svc.Me(t.Context(), u.ID)
	require.ErrorIs(t, err, dbErr)
}
