package models

import "time"

// Delivery states, as the product specification names them.
const (
	DeliveryQueued         = "queued"
	DeliveryInFlight       = "in_flight"
	DeliveryRetryScheduled = "retry_scheduled"
	DeliverySucceeded      = "succeeded"
	DeliveryFailed         = "failed"
	DeliveryCancelled      = "cancelled"
)

// Delivery is one message bound for one endpoint: the schedule authority for its attempts.
type Delivery struct {
	ID                   int64      `db:"id"`
	WorkspaceID          string     `db:"workspace_id"` // the message's workspace, kept here so the backlog counts without a join
	MessageID            int64      `db:"message_id"`
	EndpointID           int64      `db:"endpoint_id"`
	EndpointRevision     int32      `db:"endpoint_revision"`
	State                string     `db:"state"`
	AttemptCount         int32      `db:"attempt_count"`
	DueAt                *time.Time `db:"due_at"`
	DeadlineAt           time.Time  `db:"deadline_at"`
	LeaseToken           *string    `db:"lease_token"`
	LeaseExpiresAt       *time.Time `db:"lease_expires_at"`
	CancelRequested      bool       `db:"cancel_requested"`
	FailureReason        *string    `db:"failure_reason"`
	ReplayOfDeliveryID   *int64     `db:"replay_of_delivery_id"`
	ReplayIdempotencyKey *string    `db:"replay_idempotency_key"`
	CompletedAt          *time.Time `db:"completed_at"`
	CreatedAt            time.Time  `db:"created_at"`
	UpdatedAt            time.Time  `db:"updated_at"`
}

// Terminal reports whether the delivery cycle has ended.
func (d *Delivery) Terminal() bool {
	return d.State == DeliverySucceeded || d.State == DeliveryFailed || d.State == DeliveryCancelled
}

// DeliveryForList is a delivery as an endpoint's list shows it: with the event it carries and its latest attempt.
type DeliveryForList struct {
	Delivery
	PublicID      string    `db:"public_id"` // the message's webhook-id
	EventType     string    `db:"event_type"`
	SenderEventID *string   `db:"sender_event_id"`
	AcceptedAt    time.Time `db:"accepted_at"`
	// The latest attempt; LastAttemptNumber is nil until the delivery was attempted.
	LastAttemptNumber    *int32     `db:"last_attempt_number"`
	LastAttemptStartedAt *time.Time `db:"last_attempt_started_at"`
	LastAttemptLatencyMS *int32     `db:"last_attempt_latency_ms"`
	LastAttemptStatus    *int32     `db:"last_attempt_status_code"`
	LastAttemptError     *string    `db:"last_attempt_error_class"`
}

// ExpiredDelivery is an in_flight delivery whose lease ran out, re-leased by the sweep.
type ExpiredDelivery struct {
	Delivery
	LeaseExpiredAt time.Time `db:"lease_expired_at"` // when the lost lease ended
}

// DeliveryOutcome is what the worker records after one attempt, fenced by the lease token.
type DeliveryOutcome struct {
	DeliveryID    int64
	LeaseToken    string
	State         string
	DueAt         *time.Time // next attempt while retry scheduled
	FailureReason *string
}

// Attempt is one network attempt of a delivery; kept even when its result is unknown.
type Attempt struct {
	ID         int64 `db:"id"`
	DeliveryID int64 `db:"delivery_id"`
	EndpointID int64 `db:"-"` // carried for the state event, not stored

	Number           int32             `db:"number"`
	EndpointRevision int32             `db:"endpoint_revision"`
	SecretVersion    int32             `db:"secret_version"`
	RequestURL       string            `db:"request_url"`
	StartedAt        time.Time         `db:"started_at"`
	CompletedAt      *time.Time        `db:"completed_at"`
	LatencyMS        int32             `db:"latency_ms"`
	StatusCode       *int32            `db:"status_code"`
	ErrorClass       *string           `db:"error_class"`
	ResponseHeaders  map[string]string `db:"response_headers"`
	ResponsePreview  *string           `db:"response_preview"`
}
