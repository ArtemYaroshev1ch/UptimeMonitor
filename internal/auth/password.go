// Package auth hashes passwords and issues and verifies access tokens. It
// knows nothing about HTTP or storage.
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// DefaultBcryptCost is the bcrypt work factor for new hashes: 2^12 rounds,
// about 0.2 s on a modern core. That is slow enough to make offline
// brute-forcing of a leaked hash expensive and fast enough for a login.
const DefaultBcryptCost = 12

// MaxPasswordBytes is the bcrypt input limit. Longer passwords are rejected
// rather than silently truncated.
const MaxPasswordBytes = 72

// ErrPasswordMismatch is returned by Compare when the password is wrong.
var ErrPasswordMismatch = errors.New("auth: password mismatch")

// Passwords hashes and checks passwords with bcrypt.
type Passwords struct {
	cost int
}

// NewPasswords returns a Passwords using the given bcrypt cost. Production
// uses DefaultBcryptCost; tests use bcrypt.MinCost to stay fast.
func NewPasswords(cost int) Passwords {
	return Passwords{cost: cost}
}

// Hash returns the bcrypt hash of password. The hash embeds a random salt and
// the cost, so Compare needs nothing else.
func (p Passwords) Hash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), p.cost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// Compare reports whether password matches hash: nil on a match,
// ErrPasswordMismatch otherwise. The comparison takes constant time.
func (p Passwords) Compare(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return ErrPasswordMismatch
	default:
		return fmt.Errorf("auth: compare password: %w", err)
	}
}
