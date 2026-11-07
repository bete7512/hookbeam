package models

import "time"

// TokenPurposeEmailVerification marks a token that verifies a user's email address.
const TokenPurposeEmailVerification = "email_verification" //nolint:gosec // a purpose label, not a credential

// TokenPurposePasswordReset marks a token that lets a user reset their password.
const TokenPurposePasswordReset = "password_reset" //nolint:gosec // a purpose label, not a credential

// AuthToken is a single-use, expiring token issued to a user for one purpose; only its hash is stored.
type AuthToken struct {
	ID        int64      `db:"id"`
	UserID    int64      `db:"user_id"`
	RequestID int64      `db:"request_id"`
	Purpose   string     `db:"purpose"`
	TokenHash []byte     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}
