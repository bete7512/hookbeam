package services

//go:generate mockgen -source=dataplane.go -destination=service_mocks/mock_dataplane.go -package=service_mocks

import (
	"context"
	"time"

	"github.com/bete7512/hookbeam/api/models"
)

// Scope is the workspace and acting user a data plane call runs under; ActorID is empty for a key or a system read.
type Scope struct {
	WorkspaceID string
	ActorID     string
}

// AcceptMessageInput is what the data plane needs to accept one event.
type AcceptMessageInput struct {
	ApplicationID  int64
	EventType      string
	IdempotencyKey string
	SenderEventID  *string
	Payload        []byte
}

// DataPlane is the control plane's port to the delivery service, in local models; the transport is behind it.
type DataPlane interface {
	CreateApplication(ctx context.Context, scope Scope, environmentID *int64, name string) (*models.Application, error)
	GetApplication(ctx context.Context, scope Scope, id int64) (*models.Application, error)
	ListApplications(ctx context.Context, scope Scope, environmentID int64) ([]models.Application, error)
	UpdateApplication(ctx context.Context, scope Scope, id int64, in UpdateApplicationInput) (*models.Application, error)
	ArchiveApplication(ctx context.Context, scope Scope, id int64) (*models.Application, int64, int64, error)
	ListEndpoints(ctx context.Context, scope Scope, applicationID int64) ([]models.Endpoint, error)
	GetOverview(ctx context.Context, scope Scope, environmentID int64) (*models.Overview, error)
	SendTestMessage(ctx context.Context, scope Scope, endpointID int64) (*models.Message, *models.Delivery, error)
	CreateEnvironment(ctx context.Context, scope Scope, name, kind string) (*models.Environment, error)
	ListEnvironments(ctx context.Context, scope Scope) ([]models.Environment, error)
	CreateEndpoint(ctx context.Context, scope Scope, in CreateEndpointInput) (*models.Endpoint, string, int32, error)
	GetEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error)
	GetWorkspaceLimits(ctx context.Context, scope Scope) (*models.WorkspaceLimits, error)
	GetHealthEpisode(ctx context.Context, scope Scope, id int64) (*models.HealthEpisode, *models.Endpoint, error)
	PauseEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error)
	ResumeEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, error)
	UpdateEndpoint(ctx context.Context, scope Scope, in UpdateEndpointInput) (*models.Endpoint, error)
	RotateEndpointSecret(ctx context.Context, scope Scope, id int64, retireIn time.Duration) (*models.SecretRotated, error)
	ArchiveEndpoint(ctx context.Context, scope Scope, id int64) (*models.Endpoint, int64, error)
	AcceptMessage(ctx context.Context, scope Scope, in AcceptMessageInput) (*models.MessageAccepted, error)
	GetMessage(ctx context.Context, scope Scope, id int64) (*models.Message, []models.Delivery, error)
	GetMessagePayload(ctx context.Context, scope Scope, id int64) (*models.MessagePayload, error)
	ListMessages(ctx context.Context, scope Scope, in ListMessagesInput) ([]models.MessageSummary, int, error)
	ListAuditEvents(ctx context.Context, scope Scope, in ListAuditInput) ([]models.AuditEvent, int, error)
	ListAttempts(ctx context.Context, scope Scope, deliveryID int64, includePreviews bool) ([]models.Attempt, error)
	CancelDelivery(ctx context.Context, scope Scope, deliveryID int64) (*models.Delivery, error)
	ReplayDelivery(ctx context.Context, scope Scope, deliveryID int64, idempotencyKey string) (*models.Delivery, error)
}
