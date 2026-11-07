package models

import "time"

// UserIdentity links a user to an account at an external identity provider.
type UserIdentity struct {
	ID        int64     `db:"id"`
	UserID    int64     `db:"user_id"`
	Provider  string    `db:"provider"`
	Subject   string    `db:"subject"`
	Email     string    `db:"email"`
	CreatedAt time.Time `db:"created_at"`
}
