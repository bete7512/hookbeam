package models

import (
	"encoding/json"
	"time"
)

// OutboxEvent is a domain event saved in the transaction that produced it, until the worker publishes it.
type OutboxEvent struct {
	ID          int64           `db:"id"`
	Type        string          `db:"type"`
	Payload     json.RawMessage `db:"payload"`
	OccurredAt  time.Time       `db:"occurred_at"`
	PublishedAt *time.Time      `db:"published_at"`
}
