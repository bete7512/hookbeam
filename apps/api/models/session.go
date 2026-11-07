package models

import "time"

// Session is a login session. It stores the hash of its current refresh token and of
// the one before it, so a replayed old token can be detected.
type Session struct {
	ID                int64      `db:"id"`
	UserID            int64      `db:"user_id"`
	TokenVersion      int32      `db:"token_version"`
	RefreshTokenHash  []byte     `db:"refresh_token_hash"`
	PreviousTokenHash []byte     `db:"previous_token_hash"`
	ExpiresAt         time.Time  `db:"expires_at"`
	RevokedAt         *time.Time `db:"revoked_at"`
	CreatedAt         time.Time  `db:"created_at"`
}

// TokenPair is returned by a successful login or refresh.
type TokenPair struct {
	AccessToken          string
	AccessTokenExpiresAt time.Time
	RefreshToken         string
}
