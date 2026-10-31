package domain

import (
	"time"

	"github.com/google/uuid"
)

// User is a registered account.
type User struct {
	ID    uuid.UUID
	Email string
	// PasswordHash is the bcrypt hash. It never leaves the service layer.
	PasswordHash string
	CreatedAt    time.Time
}
