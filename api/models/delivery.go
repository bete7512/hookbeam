package models

import "time"

// The control plane's view of the data plane's resources, mapped from the gRPC contract.

// Application is one recipient boundary inside a workspace.
type Application struct {
	ID                      int64
	WorkspaceID             string
	EnvironmentID           int64
	EnvironmentKind         string // development or production
	Name                    string
	CreatedBy               string
	ResponsePreviewsEnabled bool
	CreatedAt               time.Time
	ArchivedAt              *time.Time
}

// MessagePayload is a message's retained body, byte for byte, and when it stops being readable.
type MessagePayload struct {
	Message   Message
	Body      []byte
	ExpiresAt time.Time
}

// Environment groups a workspace's applications.
type Environment struct {
	ID          int64
	WorkspaceID string
	Name        string
	Kind        string // development or production
	CreatedBy   string
	CreatedAt   time.Time
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
	Health        EndpointHealthStatus
	// Waiting is how many deliveries are queued or scheduled for the endpoint; nil on list reads.
	Waiting *int32
	// Secrets describes the signing secret versions by fingerprint, newest first; nil on list reads.
	Secrets []EndpointSecretInfo
}

// EndpointSecretInfo is a secret version without the secret: fingerprint and lifetime.
type EndpointSecretInfo struct {
	Version     int32
	Fingerprint string
	CreatedAt   time.Time
	RetireAt    *time.Time
}

// Overview is what an environment's overview tiles show.
type Overview struct {
	Since              time.Time
	ComputedAt         time.Time
	Accepted           int64
	Succeeded          int64
	Exhausted          int64
	Backlog            int64
	OldestPendingDueAt *time.Time
	EndpointsHealthy   int32
	EndpointsUnhealthy int32
	EndpointsUnknown   int32
	HealthObservedAt   *time.Time
	Series             []OverviewBucket
}

// OverviewBucket is one hour of the overview window.
type OverviewBucket struct {
	Hour      time.Time
	Accepted  int64
	Succeeded int64
	Failed    int64
}

// WorkspaceLimits is the data plane's quota for the workspace and where it stands.
type WorkspaceLimits struct {
	EndpointsPerApplication int
	FanoutPerMessage        int
	BacklogPerWorkspace     int
	Backlog                 int
	Applications            []ApplicationEndpointCount
	Retention               Retention
}

// Retention is how long the data plane keeps payloads, idempotency keys and audit events.
type Retention struct {
	Payload           time.Duration
	IdempotencyWindow time.Duration
	Audit             time.Duration
}

// ApplicationEndpointCount is an application's active endpoint count.
type ApplicationEndpointCount struct {
	ApplicationID int64
	Name          string
	Endpoints     int32
}

// HealthEpisode is one stretch of consecutive failures of an endpoint; ResolvedAt is nil while it is open.
type HealthEpisode struct {
	ID                  int64
	EndpointID          int64
	Revision            int32
	ConsecutiveFailures int32
	OpenedAt            time.Time
	ResolvedAt          *time.Time
}

// EndpointHealthStatus is what the data plane's attempts say about a destination.
type EndpointHealthStatus struct {
	State               string // unknown, healthy or unhealthy
	ObservedAt          *time.Time
	Stale               bool
	ConsecutiveFailures int32
}

// Message is an accepted event.
type Message struct {
	ID              int64
	WebhookID       string // the webhook-id receivers see
	ApplicationID   int64
	EnvironmentID   int64  // set on reads
	EnvironmentKind string // set on reads
	Type            string
	ExternalID      *string
	AcceptedAt      time.Time
	DeliveryCount   int32
	// PayloadExpiresAt is when the stored payload is dropped; nil when the data plane did not say.
	PayloadExpiresAt *time.Time
}

// DeliveryCounts is how many deliveries of a message are in each state.
type DeliveryCounts struct {
	Queued, InFlight, RetryScheduled, Succeeded, Failed, Cancelled int32
}

// MessageSummary is a message as the explorer lists it: metadata and counts, never the payload.
type MessageSummary struct {
	Message
	Counts           DeliveryCounts
	PayloadExpiresAt time.Time
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
	EndpointRevision   int32 // the endpoint configuration the cycle was planned under
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
	ResponsePreview  *string           // only for callers allowed to read payloads, when the application enables previews
	ResponseHeaders  map[string]string // same gate as the preview; set-cookie never arrives
}

// SecretRotated is the result of a signing-secret rotation; SigningSecret is shown once.
type SecretRotated struct {
	Endpoint      *Endpoint
	SigningSecret string
	SecretVersion int32
	RetireAt      time.Time // when the previous secrets stop signing
}
