package models

import "time"

// Message is one accepted event; Payload is the exact body receivers get.
type Message struct {
	ID                   int64      `db:"id"`
	PublicID             string     `db:"public_id"` // the webhook-id receivers see
	WorkspaceID          string     `db:"workspace_id"`
	ApplicationID        int64      `db:"application_id"`
	EventType            string     `db:"event_type"`
	SenderEventID        *string    `db:"sender_event_id"`
	IdempotencyKey       *string    `db:"idempotency_key"` // cleared when its retention ends
	IdempotencyExpiresAt *time.Time `db:"idempotency_expires_at"`
	Payload              []byte     `db:"payload"` // exact submitted bytes
	PayloadSHA256        []byte     `db:"payload_sha256"`
	PayloadExpiresAt     time.Time  `db:"payload_expires_at"`
	AcceptedAt           time.Time  `db:"accepted_at"`
}

// DeliveryCounts is how many deliveries of a message are in each state.
type DeliveryCounts struct {
	Queued         int32 `db:"n_queued"`
	InFlight       int32 `db:"n_in_flight"`
	RetryScheduled int32 `db:"n_retry_scheduled"`
	Succeeded      int32 `db:"n_succeeded"`
	Failed         int32 `db:"n_failed"`
	Cancelled      int32 `db:"n_cancelled"`
}

// MessageForList is a message as the explorer shows it: metadata and delivery counts, never the payload.
type MessageForList struct {
	ID               int64     `db:"id"`
	PublicID         string    `db:"public_id"`
	WorkspaceID      string    `db:"workspace_id"`
	ApplicationID    int64     `db:"application_id"`
	EnvironmentID    int64     `db:"environment_id"`
	EnvironmentKind  string    `db:"environment_kind"`
	EventType        string    `db:"event_type"`
	SenderEventID    *string   `db:"sender_event_id"`
	AcceptedAt       time.Time `db:"accepted_at"`
	PayloadExpiresAt time.Time `db:"payload_expires_at"`
	DeliveryCounts
}
