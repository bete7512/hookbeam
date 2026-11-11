package services

//go:generate mockgen -source=delivery.go -destination=service_mocks/mock_delivery.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// CreateEndpointInput is what a caller provides for a new endpoint.
type CreateEndpointInput struct {
	ApplicationID int64
	URL           string
	Description   string
	EventTypes    []string
}

// ListMessagesInput filters and pages the explorer; zero values mean "any".
type ListMessagesInput struct {
	ApplicationID  int64
	EnvironmentID  int64
	Type           string
	ExternalID     string
	AcceptedAfter  *time.Time
	AcceptedBefore *time.Time
	Outcome        string
	Limit          int
	Offset         int
	SortBy         string
	SortDir        string
}

// SubmitMessageInput is a raw submission; Payload is forwarded byte for byte.
type SubmitMessageInput struct {
	ApplicationID  int64
	IdempotencyKey string
	Payload        []byte
}

// UpdateEndpointInput changes the fields that are set.
type UpdateEndpointInput struct {
	EndpointID       int64
	URL              *string
	Description      *string
	EventTypes       *[]string
	ExpectedRevision *int32 // when set, the change only applies if the endpoint is still at this revision
}

// DeliveryService is the control plane's door to the data plane: it authorizes every call against the
// caller's role or key, then hands it to the DataPlane under the caller's scope.
type DeliveryService interface {
	// CreateApplication creates the application in the workspace's development environment.
	CreateApplication(ctx context.Context, actor Actor, name string) (*models.Application, error)
	// CreateApplicationIn creates the application in a given environment.
	CreateApplicationIn(ctx context.Context, actor Actor, environmentID int64, name string) (*models.Application, error)
	// UpdateApplication changes an application's settings; owners and admins.
	UpdateApplication(ctx context.Context, actor Actor, id int64, in UpdateApplicationInput) (*models.Application, error)
	// ArchiveApplication retires an application with its endpoints and their unsent deliveries; owners and admins.
	// It returns the application, the endpoints archived and the deliveries cancelled.
	ArchiveApplication(ctx context.Context, actor Actor, id int64) (*models.Application, int64, int64, error)
	GetApplication(ctx context.Context, actor Actor, id int64) (*models.Application, error)
	// ListApplications returns an environment's applications, archived ones included.
	ListApplications(ctx context.Context, actor Actor, environmentID int64) ([]models.Application, error)
	// ListEndpoints returns an application's endpoints with health, archived ones included.
	ListEndpoints(ctx context.Context, actor Actor, applicationID int64) ([]models.Endpoint, error)
	// GetOverview reads an environment's overview tiles.
	GetOverview(ctx context.Context, actor Actor, environmentID int64) (*models.Overview, error)
	// SendTestMessage plans a hookbeam.test message for one endpoint and returns it with its delivery; not viewers.
	SendTestMessage(ctx context.Context, actor Actor, endpointID int64) (*models.Message, *models.Delivery, error)
	CreateEnvironment(ctx context.Context, actor Actor, name, kind string) (*models.Environment, error)
	ListEnvironments(ctx context.Context, actor Actor) ([]models.Environment, error)
	// CreateEndpoint returns the endpoint and its signing secret, shown once.
	CreateEndpoint(ctx context.Context, actor Actor, in CreateEndpointInput) (endpoint *models.Endpoint, secret string, version int32, err error)
	// GetEndpoint reads an endpoint with its subscriptions and health.
	GetEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error)
	// GetWorkspaceLimits reports the quotas and the workspace's usage; every role.
	GetWorkspaceLimits(ctx context.Context, actor Actor) (*models.WorkspaceLimits, error)
	// GetHealthEpisode reads an episode and its endpoint for the worker; no role check, the workspace scopes it.
	GetHealthEpisode(ctx context.Context, workspaceID string, id int64) (*models.HealthEpisode, *models.Endpoint, error)
	PauseEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error)
	UpdateEndpoint(ctx context.Context, actor Actor, in UpdateEndpointInput) (*models.Endpoint, error)
	// RotateEndpointSecret returns the new secret once and when the previous ones stop signing.
	RotateEndpointSecret(ctx context.Context, actor Actor, id int64, retireIn time.Duration) (*models.SecretRotated, error)
	ResumeEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error)
	// ArchiveEndpoint retires an endpoint; cancelled is how many unsent deliveries it stopped.
	ArchiveEndpoint(ctx context.Context, actor Actor, id int64) (endpoint *models.Endpoint, cancelled int64, err error)
	// SubmitMessage reads the event type from the payload's "type" and accepts the event for the workspace's application.
	SubmitMessage(ctx context.Context, workspaceID string, in SubmitMessageInput) (*models.MessageAccepted, error)
	GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, []models.Delivery, error)
	// GetMessagePayload returns the retained bytes for a caller allowed to read payloads; expired is 410.
	GetMessagePayload(ctx context.Context, actor Actor, id int64) (*models.MessagePayload, error)
	// ListAuditEvents pages the data plane's audit trail for the workspace, newest first.
	ListAuditEvents(ctx context.Context, workspaceID string, in ListAuditInput) ([]models.AuditEvent, int, error)
	// ListMessages pages the workspace's messages; sessions and keys with messages:read.
	ListMessages(ctx context.Context, workspaceID string, in ListMessagesInput) ([]models.MessageSummary, int, error)
	// ListAttempts returns a delivery's attempts; response previews only for callers allowed to read payloads.
	ListAttempts(ctx context.Context, actor Actor, in ListAttemptsInput) ([]models.Attempt, error)
	// CancelDelivery stops further attempts of a delivery; repeating it returns the same finished delivery.
	CancelDelivery(ctx context.Context, actor Actor, deliveryID int64) (*models.Delivery, error)
	// ReplayDelivery starts a new cycle for a failed or cancelled delivery; the same key returns the same cycle.
	ReplayDelivery(ctx context.Context, actor Actor, deliveryID int64, idempotencyKey string) (*models.Delivery, error)
}

// UpdateApplicationInput carries the settings a change may touch; nil leaves one as it is.
type UpdateApplicationInput struct {
	ResponsePreviewsEnabled *bool
}

// ListAttemptsInput names the delivery and, for an API key caller, the scopes the key holds.
type ListAttemptsInput struct {
	DeliveryID int64
	KeyScopes  []string
}

// DeliveryServiceDeps are the dependencies; all are required.
type DeliveryServiceDeps struct {
	DataPlane DataPlane
	Authz     Authorizer
	Logger    *slog.Logger
}

type deliveryService struct {
	deps DeliveryServiceDeps
}

// NewDeliveryService returns a DeliveryService or an error if a dependency is missing.
func NewDeliveryService(deps DeliveryServiceDeps) (DeliveryService, error) {
	if deps.Authz == nil || deps.DataPlane == nil || deps.Logger == nil {
		return nil, errors.New("services: Authz, DataPlane and Logger are required")
	}
	return &deliveryService{deps: deps}, nil
}

// allow checks the role matrix for a user; an API key (no user) already passed the spec's scope gate in the
// middleware, so it only needs a workspace.
func (s *deliveryService) allow(ctx context.Context, actor Actor, c Capability) error {
	if actor.UserID == "" {
		if actor.WorkspaceID == "" {
			return apperr.NewForbidden("select a workspace", nil)
		}
		return nil
	}
	return s.deps.Authz.Can(ctx, actor, c)
}

// scopeOf is the data plane scope an actor's calls run under.
func scopeOf(actor Actor) Scope {
	return Scope{WorkspaceID: actor.WorkspaceID, ActorID: actor.UserID}
}

// requireWorkspace is the check for routes that accept keys too: the workspace is the whole identity.
func requireWorkspace(workspaceID string) error {
	if workspaceID == "" {
		return apperr.NewUnauthorized("a workspace is required", nil)
	}
	return nil
}

func (s *deliveryService) CreateApplication(ctx context.Context, actor Actor, name string) (*models.Application, error) {
	return s.createApplication(ctx, actor, nil, name)
}

func (s *deliveryService) CreateApplicationIn(ctx context.Context, actor Actor, environmentID int64, name string) (*models.Application, error) {
	return s.createApplication(ctx, actor, &environmentID, name)
}

func (s *deliveryService) createApplication(ctx context.Context, actor Actor, environmentID *int64, name string) (*models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.CreateApplication(ctx, scopeOf(actor), environmentID, name)
}

func (s *deliveryService) GetApplication(ctx context.Context, actor Actor, id int64) (*models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.GetApplication(ctx, scopeOf(actor), id)
}

func (s *deliveryService) ListApplications(ctx context.Context, actor Actor, environmentID int64) ([]models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ListApplications(ctx, scopeOf(actor), environmentID)
}

func (s *deliveryService) ListEndpoints(ctx context.Context, actor Actor, applicationID int64) ([]models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ListEndpoints(ctx, scopeOf(actor), applicationID)
}

func (s *deliveryService) GetOverview(ctx context.Context, actor Actor, environmentID int64) (*models.Overview, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.GetOverview(ctx, scopeOf(actor), environmentID)
}

func (s *deliveryService) SendTestMessage(ctx context.Context, actor Actor, endpointID int64) (*models.Message, *models.Delivery, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReplay); err != nil {
		return nil, nil, err
	}
	return s.deps.DataPlane.SendTestMessage(ctx, scopeOf(actor), endpointID)
}

func (s *deliveryService) UpdateApplication(ctx context.Context, actor Actor, id int64, in UpdateApplicationInput) (*models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.UpdateApplication(ctx, scopeOf(actor), id, in)
}

func (s *deliveryService) CreateEnvironment(ctx context.Context, actor Actor, name, kind string) (*models.Environment, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.CreateEnvironment(ctx, scopeOf(actor), name, kind)
}

func (s *deliveryService) ListEnvironments(ctx context.Context, actor Actor) ([]models.Environment, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ListEnvironments(ctx, scopeOf(actor))
}

func (s *deliveryService) CreateEndpoint(ctx context.Context, actor Actor, in CreateEndpointInput) (*models.Endpoint, string, int32, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, "", 0, err
	}
	return s.deps.DataPlane.CreateEndpoint(ctx, scopeOf(actor), in)
}

func (s *deliveryService) GetEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.GetEndpoint(ctx, scopeOf(actor), id)
}

func (s *deliveryService) GetWorkspaceLimits(ctx context.Context, actor Actor) (*models.WorkspaceLimits, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.GetWorkspaceLimits(ctx, scopeOf(actor))
}

func (s *deliveryService) GetHealthEpisode(ctx context.Context, workspaceID string, id int64) (*models.HealthEpisode, *models.Endpoint, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}
	return s.deps.DataPlane.GetHealthEpisode(ctx, Scope{WorkspaceID: workspaceID}, id)
}

func (s *deliveryService) PauseEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.PauseEndpoint(ctx, scopeOf(actor), id)
}

func (s *deliveryService) UpdateEndpoint(ctx context.Context, actor Actor, in UpdateEndpointInput) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.UpdateEndpoint(ctx, scopeOf(actor), in)
}

func (s *deliveryService) RotateEndpointSecret(ctx context.Context, actor Actor, id int64, retireIn time.Duration) (*models.SecretRotated, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.RotateEndpointSecret(ctx, scopeOf(actor), id, retireIn)
}

func (s *deliveryService) ResumeEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ResumeEndpoint(ctx, scopeOf(actor), id)
}

func (s *deliveryService) ArchiveApplication(ctx context.Context, actor Actor, id int64) (*models.Application, int64, int64, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, 0, 0, err
	}
	return s.deps.DataPlane.ArchiveApplication(ctx, scopeOf(actor), id)
}

func (s *deliveryService) ArchiveEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, int64, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, 0, err
	}
	return s.deps.DataPlane.ArchiveEndpoint(ctx, scopeOf(actor), id)
}

// envelope is the part of a submission Hookbeam reads; the rest travels untouched.
type envelope struct {
	Type       string  `json:"type"`
	ExternalID *string `json:"external_id"`
}

func (s *deliveryService) SubmitMessage(ctx context.Context, workspaceID string, in SubmitMessageInput) (*models.MessageAccepted, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(in.Payload, &env); err != nil || env.Type == "" {
		return nil, apperr.NewValidation(map[string]string{"body": "must be a JSON object with a string \"type\""})
	}
	return s.deps.DataPlane.AcceptMessage(ctx, Scope{WorkspaceID: workspaceID}, AcceptMessageInput{
		ApplicationID:  in.ApplicationID,
		EventType:      env.Type,
		IdempotencyKey: in.IdempotencyKey,
		SenderEventID:  env.ExternalID,
		Payload:        in.Payload,
	})
}

func (s *deliveryService) GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, []models.Delivery, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, nil, err
	}
	return s.deps.DataPlane.GetMessage(ctx, Scope{WorkspaceID: workspaceID}, id)
}

func (s *deliveryService) GetMessagePayload(ctx context.Context, actor Actor, id int64) (*models.MessagePayload, error) {
	if err := s.allow(ctx, actor, CapReadPayloads); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.GetMessagePayload(ctx, scopeOf(actor), id)
}

// canReadPayloads decides whether attempt previews may be shown: the role matrix for a user, the payloads:read
// scope for a key.
func (s *deliveryService) canReadPayloads(ctx context.Context, actor Actor, keyScopes []string) (bool, error) {
	if actor.UserID == "" {
		return slices.Contains(keyScopes, models.ScopePayloadsRead), nil
	}
	err := s.deps.Authz.Can(ctx, actor, CapReadPayloads)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, apperr.ErrForbidden):
		return false, nil
	default:
		return false, err
	}
}

func (s *deliveryService) ListMessages(ctx context.Context, workspaceID string, in ListMessagesInput) ([]models.MessageSummary, int, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, 0, err
	}
	return s.deps.DataPlane.ListMessages(ctx, Scope{WorkspaceID: workspaceID}, in)
}

func (s *deliveryService) ListAuditEvents(ctx context.Context, workspaceID string, in ListAuditInput) ([]models.AuditEvent, int, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return nil, 0, err
	}
	return s.deps.DataPlane.ListAuditEvents(ctx, Scope{WorkspaceID: workspaceID}, in)
}

func (s *deliveryService) ListAttempts(ctx context.Context, actor Actor, in ListAttemptsInput) ([]models.Attempt, error) {
	if err := requireWorkspace(actor.WorkspaceID); err != nil {
		return nil, err
	}
	previews, err := s.canReadPayloads(ctx, actor, in.KeyScopes)
	if err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ListAttempts(ctx, scopeOf(actor), in.DeliveryID, previews)
}

func (s *deliveryService) CancelDelivery(ctx context.Context, actor Actor, deliveryID int64) (*models.Delivery, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReplay); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.CancelDelivery(ctx, scopeOf(actor), deliveryID)
}

func (s *deliveryService) ReplayDelivery(ctx context.Context, actor Actor, deliveryID int64, idempotencyKey string) (*models.Delivery, error) {
	if err := s.allow(ctx, actor, CapReplay); err != nil {
		return nil, err
	}
	return s.deps.DataPlane.ReplayDelivery(ctx, scopeOf(actor), deliveryID, idempotencyKey)
}
