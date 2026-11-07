// Package models holds the api service's domain types.
package models

import (
	"time"
)

// User is an account.
type User struct {
	ID              int64      `db:"id"`
	Email           string     `db:"email"`
	Name            string     `db:"name"`
	PasswordHash    *string    `db:"password_hash"`
	EmailVerifiedAt *time.Time `db:"email_verified_at"`
	TokenVersion    int32      `db:"token_version"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

// HasPassword reports whether the account can log in with a password.
func (u User) HasPassword() bool { return u.PasswordHash != nil }

// UserForList is a user as shown in lists, without sensitive fields.
type UserForList struct {
	ID              int64      `db:"id"`
	Email           string     `db:"email"`
	Name            string     `db:"name"`
	EmailVerifiedAt *time.Time `db:"email_verified_at"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}
