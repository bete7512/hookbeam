package models

import (
	"encoding/json"
	"time"
)

// AuditEvent is one recorded change: who did what to which resource, written with the change itself.
// Details carry safe context (names, ids, states), never secrets or payload bytes.
type AuditEvent struct {
	ID           int64           `db:"id"`
	WorkspaceID  string          `db:"workspace_id"`
	ActorID      string          `db:"actor_id"` // goauth user id, or "" for an API key
	Action       string          `db:"action"`   // dotted verb such as endpoint.pause
	ResourceType string          `db:"resource_type"`
	ResourceID   *int64          `db:"resource_id"`
	RequestID    string          `db:"request_id"`
	Details      json.RawMessage `db:"details"`
	OccurredAt   time.Time       `db:"occurred_at"`
}
