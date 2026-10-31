package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestPasswords(t *testing.T) {
	t.Parallel()

	p := NewPasswords(bcrypt.MinCost)
	hash, err := p.Hash("correct horse")
	require.NoError(t, err)
	assert.NotContains(t, hash, "correct horse")

	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{name: "match", password: "correct horse"},
		{name: "wrong password", password: "battery staple", wantErr: ErrPasswordMismatch},
		{name: "case matters", password: "Correct horse", wantErr: ErrPasswordMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := p.Compare(hash, tt.password)

			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestPasswordsSaltedHashes(t *testing.T) {
	t.Parallel()

	p := NewPasswords(bcrypt.MinCost)
	a, err := p.Hash("same password")
	require.NoError(t, err)
	b, err := p.Hash("same password")
	require.NoError(t, err)

	assert.NotEqual(t, a, b, "every hash has its own salt")
}

func TestPasswordsMalformedHash(t *testing.T) {
	t.Parallel()

	err := NewPasswords(bcrypt.MinCost).Compare("not-a-bcrypt-hash", "password")

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPasswordMismatch, "a broken hash is an internal error, not a wrong password")
}

func TestPasswordsTooLong(t *testing.T) {
	t.Parallel()

	_, err := NewPasswords(bcrypt.MinCost).Hash(string(make([]byte, MaxPasswordBytes+1)))

	require.Error(t, err)
}
