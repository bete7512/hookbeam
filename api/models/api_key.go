package models

import "time"

// API key scopes a sender credential may carry, as the spec declares them on operations.
const (
	ScopeMessagesWrite = "messages:write" // submit events
	ScopeMessagesRead  = "messages:read"  // read messages, deliveries and attempts
)

// APIKeyScopes lists every scope a key may be granted.
var APIKeyScopes = []string{ScopeMessagesWrite, ScopeMessagesRead}

// APIKeyPrefix starts every raw key so the middleware can tell keys from JWTs.
const APIKeyPrefix = "hb_"

// APIKey is a sender credential bound to a workspace; only the hash of the raw key is stored.
type APIKey struct {
	ID          int64      `db:"id"`
	WorkspaceID string     `db:"workspace_id"` // goauth organization id
	Name        string     `db:"name"`
	Prefix      string     `db:"prefix"` // first characters of the raw key, for display
	KeyHash     string     `db:"key_hash"`
	Scopes      []string   `db:"scopes"`
	CreatedBy   string     `db:"created_by"` // goauth user id
	ExpiresAt   *time.Time `db:"expires_at"`
	LastUsedAt  *time.Time `db:"last_used_at"`
	RevokedAt   *time.Time `db:"revoked_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// APIKeyForList is an API key as shown in lists, without the hash.
type APIKeyForList struct {
	ID          int64      `db:"id"`
	WorkspaceID string     `db:"workspace_id"`
	Name        string     `db:"name"`
	Prefix      string     `db:"prefix"`
	Scopes      []string   `db:"scopes"`
	CreatedBy   string     `db:"created_by"`
	ExpiresAt   *time.Time `db:"expires_at"`
	LastUsedAt  *time.Time `db:"last_used_at"`
	RevokedAt   *time.Time `db:"revoked_at"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}
