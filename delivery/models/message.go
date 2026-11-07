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
