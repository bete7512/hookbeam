package models

import "time"

// The control plane's view of the data plane's resources, mapped from the gRPC contract.

// Application is one recipient boundary inside a workspace.
type Application struct {
	ID          int64
	WorkspaceID string
	Name        string
	CreatedBy   string
	CreatedAt   time.Time
	ArchivedAt  *time.Time
}

// Endpoint is a destination URL of an application with its subscriptions.
type Endpoint struct {
	ID            int64
	ApplicationID int64
	URL           string
	Description   string
	Revision      int32
	EventTypes    []string
	PausedAt      *time.Time
	ArchivedAt    *time.Time
	CreatedAt     time.Time
}

// Message is an accepted event.
type Message struct {
	ID            int64
	WebhookID     string // the webhook-id receivers see
	ApplicationID int64
	Type          string
	ExternalID    *string
	AcceptedAt    time.Time
	DeliveryCount int32
}

// MessageAccepted is the answer to a submission.
type MessageAccepted struct {
	Message   Message
	Duplicate bool
}

// Delivery is one message bound for one endpoint.
type Delivery struct {
	ID                 int64
	MessageID          int64
	EndpointID         int64
	State              string
	AttemptCount       int32
	DueAt              *time.Time
	DeadlineAt         time.Time
	FailureReason      *string
	ReplayOfDeliveryID *int64
	CreatedAt          time.Time
	CompletedAt        *time.Time
}

// Attempt is one network attempt of a delivery.
type Attempt struct {
	ID               int64
	DeliveryID       int64
	Number           int32
	StartedAt        time.Time
	CompletedAt      *time.Time
	StatusCode       *int32
	ErrorClass       *string
	LatencyMS        int32
	SecretVersion    int32
	EndpointRevision int32
	RequestURL       string
}

// SecretRotated is the result of a signing-secret rotation; SigningSecret is shown once.
type SecretRotated struct {
	Endpoint      *Endpoint
	SigningSecret string
	SecretVersion int32
	RetireAt      time.Time // when the previous secrets stop signing
}
