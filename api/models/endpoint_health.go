package models

import "time"

// EndpointHealth is the control plane's latest observation of an endpoint, built from delivery state events.
type EndpointHealth struct {
	EndpointID          int64     `db:"endpoint_id"`
	WorkspaceID         string    `db:"workspace_id"`
	ApplicationID       int64     `db:"application_id"`
	ConsecutiveFailures int32     `db:"consecutive_failures"`
	LastState           string    `db:"last_state"`
	LastReason          *string   `db:"last_reason"`
	LastStatusCode      *int32    `db:"last_status_code"`
	LastDeliveryID      int64     `db:"last_delivery_id"`
	LastAttempt         int32     `db:"last_attempt"`
	ObservedAt          time.Time `db:"observed_at"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

// HealthObservation is one delivery outcome as the control plane counts it.
type HealthObservation struct {
	EndpointID     int64
	WorkspaceID    string
	ApplicationID  int64
	DeliveryID     int64
	Attempt        int32
	State          string
	Reason         *string
	StatusCode     *int32
	ObservedAt     time.Time
	CountsAsFailed bool // a completed attempt the receiver did not accept
}
