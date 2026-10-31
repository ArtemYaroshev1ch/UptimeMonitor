package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

var (
	testSecret = []byte("0123456789abcdef0123456789abcdef")
	testNow    = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	testUserID = uuid.MustParse("6f1c2a52-3b0e-4f63-9a59-0a8e3c1d2b4f")
)

func TestTokensRoundTrip(t *testing.T) {
	t.Parallel()

	tokens := NewTokens(testSecret, time.Hour, clock.NewFake(testNow))

	token, expiresAt, err := tokens.Issue(testUserID)
	require.NoError(t, err)
	assert.Equal(t, testNow.Add(time.Hour), expiresAt)

	got, err := tokens.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, testUserID, got)
}

func TestTokensExpiry(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(testNow)
	tokens := NewTokens(testSecret, time.Hour, clk)
	token, _, err := tokens.Issue(testUserID)
	require.NoError(t, err)

	clk.Advance(time.Hour - time.Second)
	_, err = tokens.Verify(token)
	require.NoError(t, err, "still valid just before expiry")

	clk.Advance(2 * time.Second)
	_, err = tokens.Verify(token)
	require.ErrorIs(t, err, ErrInvalidToken)
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

// sign builds a token with arbitrary claims and method: the kind of token an
// attacker might send.
func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	require.NoError(t, err)
	return s
}

func claimsWith(edit func(*jwt.RegisteredClaims)) jwt.RegisteredClaims {
	c := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   testUserID.String(),
		IssuedAt:  jwt.NewNumericDate(testNow),
		ExpiresAt: jwt.NewNumericDate(testNow.Add(time.Hour)),
	}
	edit(&c)
	return c
}

func TestTokensRejected(t *testing.T) {
	t.Parallel()

	valid := claimsWith(func(*jwt.RegisteredClaims) {})
	hs256 := jwt.SigningMethodHS256

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "garbage", token: "not.a.jwt"},
		{
			name:  "wrong secret",
			token: sign(t, hs256, []byte("another-secret-another-secret-xx"), valid),
		},
		{
			// The classic JWT attack: a token without a signature.
			name:  "alg none",
			token: sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid),
		},
		{
			// The right key, but an algorithm the server did not choose.
			name:  "other HMAC algorithm",
			token: sign(t, jwt.SigningMethodHS512, testSecret, valid),
		},
		{
			name: "expired",
			token: sign(t, hs256, testSecret, claimsWith(func(c *jwt.RegisteredClaims) {
				c.ExpiresAt = jwt.NewNumericDate(testNow.Add(-time.Second))
			})),
		},
		{
			name:  "no expiry",
			token: sign(t, hs256, testSecret, claimsWith(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil })),
		},
		{
			name: "issued in the future",
			token: sign(t, hs256, testSecret, claimsWith(func(c *jwt.RegisteredClaims) {
				c.IssuedAt = jwt.NewNumericDate(testNow.Add(time.Minute))
			})),
		},
		{
			name:  "wrong issuer",
			token: sign(t, hs256, testSecret, claimsWith(func(c *jwt.RegisteredClaims) { c.Issuer = "someone-else" })),
		},
		{
			name:  "subject is not a user ID",
			token: sign(t, hs256, testSecret, claimsWith(func(c *jwt.RegisteredClaims) { c.Subject = "admin" })),
		},
	}

	tokens := NewTokens(testSecret, time.Hour, clock.NewFake(testNow))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tokens.Verify(tt.token)

			require.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}
