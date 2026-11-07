package models

import "time"

// SubscribeAll is the event type that subscribes an endpoint to every event.
const SubscribeAll = "*"

// Endpoint is a destination URL of an application with its subscriptions.
type Endpoint struct {
	ID            int64      `db:"id"`
	WorkspaceID   string     `db:"workspace_id"`
	ApplicationID int64      `db:"application_id"`
	URL           string     `db:"url"`
	Description   string     `db:"description"`
	Revision      int32      `db:"revision"`
	PausedAt      *time.Time `db:"paused_at"`
	ArchivedAt    *time.Time `db:"archived_at"`
	CreatedBy     string     `db:"created_by"`
	CreatedAt     time.Time  `db:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at"`
	// EventTypes come from subscriptions, not from the endpoints table; empty means every event type.
	EventTypes []string `db:"-"`
}

// EndpointSecret is one version of an endpoint's signing secret, encrypted at rest.
type EndpointSecret struct {
	ID          int64      `db:"id"`
	EndpointID  int64      `db:"endpoint_id"`
	Version     int32      `db:"version"`
	Ciphertext  []byte     `db:"ciphertext"`
	KeyID       string     `db:"key_id"`      // encryption key that produced Ciphertext
	Fingerprint string     `db:"fingerprint"` // short SHA-256 prefix shown in the console
	CreatedAt   time.Time  `db:"created_at"`
	RetireAt    *time.Time `db:"retire_at"`
}

// Subscription binds an endpoint to one event type, or to every type with SubscribeAll.
type Subscription struct {
	ID         int64     `db:"id"`
	EndpointID int64     `db:"endpoint_id"`
	EventType  string    `db:"event_type"`
	CreatedAt  time.Time `db:"created_at"`
}
