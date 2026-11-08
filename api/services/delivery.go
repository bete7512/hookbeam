package services

//go:generate mockgen -source=delivery.go -destination=service_mocks/mock_delivery.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/models"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
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

// DeliveryService is the control plane's door to the data plane: it scopes every call to the caller's
// workspace, forwards it over gRPC and maps the answer back into local models and errors.
type DeliveryService interface {
	// CreateApplication creates the application in the workspace's development environment.
	CreateApplication(ctx context.Context, actor Actor, name string) (*models.Application, error)
	// CreateApplicationIn creates the application in a given environment.
	CreateApplicationIn(ctx context.Context, actor Actor, environmentID int64, name string) (*models.Application, error)
	// UpdateApplication changes an application's settings; owners and admins.
	UpdateApplication(ctx context.Context, actor Actor, id int64, in UpdateApplicationInput) (*models.Application, error)
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
	Client  deliveryv1.DeliveryServiceClient
	Authz   Authorizer
	Timeout time.Duration
	Logger  *slog.Logger
}

type deliveryService struct {
	deps DeliveryServiceDeps
}

// NewDeliveryService returns a DeliveryService or an error if a dependency is missing.
func NewDeliveryService(deps DeliveryServiceDeps) (DeliveryService, error) {
	if deps.Authz == nil || deps.Client == nil || deps.Timeout <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Authz, Client, positive Timeout and Logger are required")
	}
	return &deliveryService{deps: deps}, nil
}

// call scopes ctx to the workspace and actor and bounds it.
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

func (s *deliveryService) call(ctx context.Context, workspaceID, actorID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, s.deps.Timeout)
	return grpcx.WithScope(ctx, grpcx.Scope{WorkspaceID: workspaceID, ActorID: actorID}), cancel
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
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CreateApplication(ctx, &deliveryv1.CreateApplicationRequest{Name: name, EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (s *deliveryService) GetApplication(ctx context.Context, actor Actor, id int64) (*models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.GetApplication(ctx, &deliveryv1.GetApplicationRequest{ApplicationId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (s *deliveryService) ListApplications(ctx context.Context, actor Actor, environmentID int64) ([]models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ListApplications(ctx, &deliveryv1.ListApplicationsRequest{EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Application, 0, len(resp.GetApplications()))
	for _, a := range resp.GetApplications() {
		out = append(out, *toApplication(a))
	}
	return out, nil
}

func (s *deliveryService) ListEndpoints(ctx context.Context, actor Actor, applicationID int64) ([]models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ListEndpoints(ctx, &deliveryv1.ListEndpointsRequest{ApplicationId: applicationID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Endpoint, 0, len(resp.GetEndpoints()))
	for _, e := range resp.GetEndpoints() {
		out = append(out, *toEndpoint(e))
	}
	return out, nil
}

func (s *deliveryService) GetOverview(ctx context.Context, actor Actor, environmentID int64) (*models.Overview, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.GetOverview(ctx, &deliveryv1.GetOverviewRequest{EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	o := resp.GetOverview()
	return &models.Overview{
		Since: o.GetSince().AsTime(), ComputedAt: o.GetComputedAt().AsTime(), Accepted: o.GetAccepted(), Succeeded: o.GetSucceeded(), Exhausted: o.GetExhausted(), Backlog: o.GetBacklog(),
		OldestPendingDueAt: optionalTime(o.OldestPendingDueAt), EndpointsHealthy: o.GetEndpointsHealthy(), EndpointsUnhealthy: o.GetEndpointsUnhealthy(), EndpointsUnknown: o.GetEndpointsUnknown(), HealthObservedAt: optionalTime(o.HealthObservedAt),
	}, nil
}

func (s *deliveryService) SendTestMessage(ctx context.Context, actor Actor, endpointID int64) (*models.Message, *models.Delivery, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReplay); err != nil {
		return nil, nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.SendTestMessage(ctx, &deliveryv1.SendTestMessageRequest{EndpointId: endpointID})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return toMessage(resp.GetMessage()), &d, nil
}

func (s *deliveryService) UpdateApplication(ctx context.Context, actor Actor, id int64, in UpdateApplicationInput) (*models.Application, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.UpdateApplication(ctx, &deliveryv1.UpdateApplicationRequest{ApplicationId: id, ResponsePreviewsEnabled: in.ResponsePreviewsEnabled})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (s *deliveryService) CreateEnvironment(ctx context.Context, actor Actor, name, kind string) (*models.Environment, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CreateEnvironment(ctx, &deliveryv1.CreateEnvironmentRequest{Name: name, Kind: kind})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEnvironment(resp.GetEnvironment()), nil
}

func (s *deliveryService) ListEnvironments(ctx context.Context, actor Actor) ([]models.Environment, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ListEnvironments(ctx, &deliveryv1.ListEnvironmentsRequest{})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Environment, 0, len(resp.GetEnvironments()))
	for _, e := range resp.GetEnvironments() {
		out = append(out, *toEnvironment(e))
	}
	return out, nil
}

func toEnvironment(e *deliveryv1.Environment) *models.Environment {
	return &models.Environment{ID: e.GetId(), WorkspaceID: e.GetWorkspaceId(), Name: e.GetName(), Kind: e.GetKind(), CreatedBy: e.GetCreatedBy(), CreatedAt: e.GetCreatedAt().AsTime()}
}

func (s *deliveryService) CreateEndpoint(ctx context.Context, actor Actor, in CreateEndpointInput) (*models.Endpoint, string, int32, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, "", 0, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CreateEndpoint(ctx, &deliveryv1.CreateEndpointRequest{ApplicationId: in.ApplicationID, Url: in.URL, Description: in.Description, EventTypes: in.EventTypes})
	if err != nil {
		return nil, "", 0, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), resp.GetSigningSecret(), resp.GetSecretVersion(), nil
}

func (s *deliveryService) GetEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.GetEndpoint(ctx, &deliveryv1.GetEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (s *deliveryService) GetWorkspaceLimits(ctx context.Context, actor Actor) (*models.WorkspaceLimits, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.GetWorkspaceLimits(ctx, &deliveryv1.GetWorkspaceLimitsRequest{})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	l := resp.GetLimits()
	out := &models.WorkspaceLimits{
		EndpointsPerApplication: int(l.GetEndpointsPerApplication()), FanoutPerMessage: int(l.GetFanoutPerMessage()), BacklogPerWorkspace: int(l.GetBacklogPerWorkspace()),
		Backlog: int(resp.GetBacklog()), Applications: make([]models.ApplicationEndpointCount, 0, len(resp.GetApplications())),
	}
	for _, a := range resp.GetApplications() {
		out.Applications = append(out.Applications, models.ApplicationEndpointCount{ApplicationID: a.GetApplicationId(), Name: a.GetName(), Endpoints: a.GetEndpoints()})
	}
	return out, nil
}

func (s *deliveryService) GetHealthEpisode(ctx context.Context, workspaceID string, id int64) (*models.HealthEpisode, *models.Endpoint, error) {
	if workspaceID == "" {
		return nil, nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	resp, err := s.deps.Client.GetHealthEpisode(ctx, &deliveryv1.GetHealthEpisodeRequest{EpisodeId: id})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	e := resp.GetEpisode()
	episode := &models.HealthEpisode{ID: e.GetId(), EndpointID: e.GetEndpointId(), Revision: e.GetRevision(), ConsecutiveFailures: e.GetConsecutiveFailures(), OpenedAt: e.GetOpenedAt().AsTime(), ResolvedAt: optionalTime(e.ResolvedAt)}
	return episode, toEndpoint(resp.GetEndpoint()), nil
}

func (s *deliveryService) PauseEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.PauseEndpoint(ctx, &deliveryv1.PauseEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

// envelope is the part of a submission Hookbeam reads; the rest travels untouched.
func (s *deliveryService) UpdateEndpoint(ctx context.Context, actor Actor, in UpdateEndpointInput) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	req := &deliveryv1.UpdateEndpointRequest{EndpointId: in.EndpointID, Url: in.URL, Description: in.Description, ExpectedRevision: in.ExpectedRevision}
	if in.EventTypes != nil {
		req.EventTypes = &deliveryv1.EventTypeList{Values: *in.EventTypes}
	}
	resp, err := s.deps.Client.UpdateEndpoint(ctx, req)
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (s *deliveryService) RotateEndpointSecret(ctx context.Context, actor Actor, id int64, retireIn time.Duration) (*models.SecretRotated, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.RotateEndpointSecret(ctx, &deliveryv1.RotateEndpointSecretRequest{EndpointId: id, RetireIn: durationpb.New(retireIn)})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.SecretRotated{Endpoint: toEndpoint(resp.GetEndpoint()), SigningSecret: resp.GetSigningSecret(), SecretVersion: resp.GetSecretVersion(), RetireAt: resp.GetRetireAt().AsTime()}, nil
}

func (s *deliveryService) ResumeEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ResumeEndpoint(ctx, &deliveryv1.ResumeEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (s *deliveryService) ArchiveEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, int64, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageEndpoints); err != nil {
		return nil, 0, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ArchiveEndpoint(ctx, &deliveryv1.ArchiveEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), resp.GetCancelledDeliveries(), nil
}

type envelope struct {
	Type       string  `json:"type"`
	ExternalID *string `json:"external_id"`
}

func (s *deliveryService) SubmitMessage(ctx context.Context, workspaceID string, in SubmitMessageInput) (*models.MessageAccepted, error) {
	if workspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	var env envelope
	if err := json.Unmarshal(in.Payload, &env); err != nil || env.Type == "" {
		return nil, apperr.NewValidation(map[string]string{"body": "must be a JSON object with a string \"type\""})
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	resp, err := s.deps.Client.AcceptMessage(ctx, &deliveryv1.AcceptMessageRequest{
		ApplicationId:  in.ApplicationID,
		EventType:      env.Type,
		IdempotencyKey: in.IdempotencyKey,
		SenderEventId:  env.ExternalID,
		Payload:        in.Payload,
	})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.MessageAccepted{Message: *toMessage(resp.GetMessage()), Duplicate: resp.GetDuplicate()}, nil
}

func (s *deliveryService) GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, []models.Delivery, error) {
	if workspaceID == "" {
		return nil, nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	resp, err := s.deps.Client.GetMessage(ctx, &deliveryv1.GetMessageRequest{MessageId: id})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	deliveries := make([]models.Delivery, 0, len(resp.GetDeliveries()))
	for _, d := range resp.GetDeliveries() {
		deliveries = append(deliveries, toDelivery(d))
	}
	return toMessage(resp.GetMessage()), deliveries, nil
}

func (s *deliveryService) GetMessagePayload(ctx context.Context, actor Actor, id int64) (*models.MessagePayload, error) {
	if err := s.allow(ctx, actor, CapReadPayloads); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.GetMessagePayload(ctx, &deliveryv1.GetMessagePayloadRequest{MessageId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.MessagePayload{Message: *toMessage(resp.GetMessage()), Body: resp.GetPayload(), ExpiresAt: resp.GetPayloadExpiresAt().AsTime()}, nil
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
	if workspaceID == "" {
		return nil, 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	req := &deliveryv1.ListMessagesRequest{
		ApplicationId: in.ApplicationID, EnvironmentId: in.EnvironmentID, EventType: in.Type, SenderEventId: in.ExternalID, Outcome: in.Outcome,
		Limit: int32(in.Limit), Offset: int32(in.Offset), SortBy: in.SortBy, SortDir: in.SortDir, //nolint:gosec // validated page sizes
	}
	if in.AcceptedAfter != nil {
		req.AcceptedAfter = timestamppb.New(*in.AcceptedAfter)
	}
	if in.AcceptedBefore != nil {
		req.AcceptedBefore = timestamppb.New(*in.AcceptedBefore)
	}
	resp, err := s.deps.Client.ListMessages(ctx, req)
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	items := make([]models.MessageSummary, 0, len(resp.GetMessages()))
	for _, m := range resp.GetMessages() {
		c := m.GetCounts()
		items = append(items, models.MessageSummary{
			Message:          *toMessage(m.GetMessage()),
			Counts:           models.DeliveryCounts{Queued: c.GetQueued(), InFlight: c.GetInFlight(), RetryScheduled: c.GetRetryScheduled(), Succeeded: c.GetSucceeded(), Failed: c.GetFailed(), Cancelled: c.GetCancelled()},
			PayloadExpiresAt: m.GetPayloadExpiresAt().AsTime(),
		})
	}
	return items, int(resp.GetTotal()), nil
}

func (s *deliveryService) ListAuditEvents(ctx context.Context, workspaceID string, in ListAuditInput) ([]models.AuditEvent, int, error) {
	if workspaceID == "" {
		return nil, 0, apperr.NewUnauthorized("a workspace is required", nil)
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	req := &deliveryv1.ListAuditEventsRequest{ActorId: in.ActorID, ResourceType: in.ResourceType, ResourceId: in.ResourceID, Limit: int32(in.Limit), Offset: int32(in.Offset)} //nolint:gosec // validated page sizes
	if in.After != nil {
		req.After = timestamppb.New(*in.After)
	}
	if in.Before != nil {
		req.Before = timestamppb.New(*in.Before)
	}
	resp, err := s.deps.Client.ListAuditEvents(ctx, req)
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	out := make([]models.AuditEvent, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		out = append(out, models.AuditEvent{ID: e.GetId(), WorkspaceID: e.GetWorkspaceId(), ActorID: e.GetActorId(), Action: e.GetAction(), ResourceType: e.GetResourceType(), ResourceID: e.ResourceId, RequestID: e.GetRequestId(), Details: e.GetDetails(), OccurredAt: e.GetOccurredAt().AsTime()})
	}
	return out, int(resp.GetTotal()), nil
}

func (s *deliveryService) ListAttempts(ctx context.Context, actor Actor, in ListAttemptsInput) ([]models.Attempt, error) {
	if actor.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	previews, err := s.canReadPayloads(ctx, actor, in.KeyScopes)
	if err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ListAttempts(ctx, &deliveryv1.ListAttemptsRequest{DeliveryId: in.DeliveryID, IncludePreviews: previews})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	attempts := make([]models.Attempt, 0, len(resp.GetAttempts()))
	for _, a := range resp.GetAttempts() {
		attempts = append(attempts, toAttempt(a))
	}
	return attempts, nil
}

func (s *deliveryService) CancelDelivery(ctx context.Context, actor Actor, deliveryID int64) (*models.Delivery, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReplay); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CancelDelivery(ctx, &deliveryv1.CancelDeliveryRequest{DeliveryId: deliveryID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return &d, nil
}

func (s *deliveryService) ReplayDelivery(ctx context.Context, actor Actor, deliveryID int64, idempotencyKey string) (*models.Delivery, error) {
	if err := s.allow(ctx, actor, CapReplay); err != nil {
		return nil, err
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.ReplayDelivery(ctx, &deliveryv1.ReplayDeliveryRequest{DeliveryId: deliveryID, IdempotencyKey: idempotencyKey})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return &d, nil
}

func toApplication(a *deliveryv1.Application) *models.Application {
	return &models.Application{ID: a.GetId(), WorkspaceID: a.GetWorkspaceId(), EnvironmentID: a.GetEnvironmentId(), EnvironmentKind: a.GetEnvironmentKind(), Name: a.GetName(), CreatedBy: a.GetCreatedBy(), ResponsePreviewsEnabled: a.GetResponsePreviewsEnabled(), CreatedAt: a.GetCreatedAt().AsTime(), ArchivedAt: optionalTime(a.ArchivedAt)}
}

func toEndpoint(e *deliveryv1.Endpoint) *models.Endpoint {
	types := e.GetEventTypes()
	if types == nil {
		types = []string{}
	}
	health := models.EndpointHealthStatus{State: "unknown"}
	if h := e.GetHealth(); h != nil {
		health = models.EndpointHealthStatus{State: h.GetState(), ObservedAt: optionalTime(h.ObservedAt), Stale: h.GetStale(), ConsecutiveFailures: h.GetConsecutiveFailures()}
	}
	out := &models.Endpoint{
		ID: e.GetId(), ApplicationID: e.GetApplicationId(), URL: e.GetUrl(), Description: e.GetDescription(), Revision: e.GetRevision(),
		EventTypes: types, PausedAt: optionalTime(e.PausedAt), ArchivedAt: optionalTime(e.ArchivedAt), CreatedAt: e.GetCreatedAt().AsTime(), Health: health, Waiting: e.Waiting,
	}
	if e.Secrets != nil {
		out.Secrets = make([]models.EndpointSecretInfo, 0, len(e.Secrets))
		for _, sec := range e.Secrets {
			out.Secrets = append(out.Secrets, models.EndpointSecretInfo{Version: sec.GetVersion(), Fingerprint: sec.GetFingerprint(), CreatedAt: sec.GetCreatedAt().AsTime(), RetireAt: optionalTime(sec.RetireAt)})
		}
	}
	return out
}

func toMessage(m *deliveryv1.Message) *models.Message {
	return &models.Message{ID: m.GetId(), WebhookID: m.GetPublicId(), ApplicationID: m.GetApplicationId(), EnvironmentID: m.GetEnvironmentId(), EnvironmentKind: m.GetEnvironmentKind(), Type: m.GetEventType(), ExternalID: m.SenderEventId, AcceptedAt: m.GetAcceptedAt().AsTime(), DeliveryCount: m.GetDeliveryCount()}
}

var deliveryStates = map[deliveryv1.DeliveryState]string{
	deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED:          "queued",
	deliveryv1.DeliveryState_DELIVERY_STATE_IN_FLIGHT:       "in_flight",
	deliveryv1.DeliveryState_DELIVERY_STATE_RETRY_SCHEDULED: "retry_scheduled",
	deliveryv1.DeliveryState_DELIVERY_STATE_SUCCEEDED:       "succeeded",
	deliveryv1.DeliveryState_DELIVERY_STATE_FAILED:          "failed",
	deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED:       "cancelled",
}

func toDelivery(d *deliveryv1.Delivery) models.Delivery {
	return models.Delivery{
		ID: d.GetId(), MessageID: d.GetMessageId(), EndpointID: d.GetEndpointId(), State: deliveryStates[d.GetState()], AttemptCount: d.GetAttemptCount(),
		DueAt: optionalTime(d.DueAt), DeadlineAt: d.GetDeadlineAt().AsTime(), FailureReason: d.FailureReason, ReplayOfDeliveryID: d.ReplayOfDeliveryId,
		CreatedAt: d.GetCreatedAt().AsTime(), CompletedAt: optionalTime(d.CompletedAt),
	}
}

func toAttempt(a *deliveryv1.Attempt) models.Attempt {
	return models.Attempt{
		ID: a.GetId(), DeliveryID: a.GetDeliveryId(), Number: a.GetNumber(), StartedAt: a.GetStartedAt().AsTime(), CompletedAt: optionalTime(a.CompletedAt),
		StatusCode: a.StatusCode, ErrorClass: a.ErrorClass, LatencyMS: a.GetLatencyMs(), SecretVersion: a.GetSecretVersion(), EndpointRevision: a.GetEndpointRevision(), RequestURL: a.GetRequestUrl(),
		ResponsePreview: a.ResponsePreview,
	}
}

func optionalTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := t.AsTime()
	return &v
}
