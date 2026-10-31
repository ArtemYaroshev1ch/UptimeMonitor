package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/artemyaroshev1ch/uptime-monitor/internal/platform/clock"
)

// issuer is the "iss" claim of every token. Checking it rejects tokens that
// were signed with the same secret for some other purpose.
const issuer = "uptime-monitor"

// ErrInvalidToken is returned by Verify for any token that must not be
// accepted: malformed, badly signed, expired, wrong algorithm or issuer.
var ErrInvalidToken = errors.New("auth: invalid token")

// Tokens issues and verifies access tokens: HS256-signed JWTs whose subject
// is the user ID.
type Tokens struct {
	secret []byte
	ttl    time.Duration
	clock  clock.Clock
	parser *jwt.Parser
}

// NewTokens returns Tokens signing with secret. Issued tokens expire after ttl.
func NewTokens(secret []byte, ttl time.Duration, clk clock.Clock) *Tokens {
	return &Tokens{
		secret: secret,
		ttl:    ttl,
		clock:  clk,
		parser: jwt.NewParser(
			// Accept HS256 only. Otherwise the token itself would choose the
			// algorithm, e.g. "none" (no signature at all).
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(issuer),
			// A token without "exp" would be valid forever.
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
			jwt.WithTimeFunc(clk.Now),
		),
	}
}

// TTL is the lifetime of issued tokens.
func (t *Tokens) TTL() time.Duration { return t.ttl }

// Issue returns a signed token for userID and its expiry time.
func (t *Tokens) Issue(userID uuid.UUID) (string, time.Time, error) {
	now := t.clock.Now()
	expiresAt := now.Add(t.ttl)
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify checks the signature and claims of token and returns its user ID.
// Every failure wraps ErrInvalidToken.
func (t *Tokens) Verify(token string) (uuid.UUID, error) {
	var claims jwt.RegisteredClaims
	_, err := t.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return t.secret, nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: subject: %w", ErrInvalidToken, err)
	}
	return userID, nil
}
