// Package rpc implements the DeliveryService gRPC contract over the services; it is imported only by cmd/server.
package rpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// Deps are the server dependencies; all are required.
type Deps struct {
	Audit        services.AuditService
	Environments services.EnvironmentService
	Applications services.ApplicationService
	Endpoints    services.EndpointService
	Health       services.HealthService
	Limits       services.LimitService
	Overview     services.OverviewService
	Messages     services.MessageService
	Logger       *slog.Logger
}

// Server serves hookbeam.delivery.v1.DeliveryService.
type Server struct {
	deliveryv1.UnimplementedDeliveryServiceServer
	deps Deps
}

// New returns a Server or an error if a dependency is missing.
func New(deps Deps) (*Server, error) {
	if deps.Audit == nil || deps.Environments == nil || deps.Applications == nil || deps.Endpoints == nil || deps.Health == nil || deps.Limits == nil || deps.Overview == nil || deps.Messages == nil || deps.Logger == nil {
		return nil, errors.New("rpc: Audit, Environments, Applications, Endpoints, Health, Limits, Overview, Messages and Logger are required")
	}
	return &Server{deps: deps}, nil
}

// CreateApplication registers a consumer application in the caller's workspace.
func (s *Server) CreateApplication(ctx context.Context, req *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Applications.CreateApplication(ctx, scope, req.GetEnvironmentId(), req.GetName())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateApplicationResponse{Application: toApplication(a)}, nil
}

// GetApplication reads one application.
func (s *Server) GetApplication(ctx context.Context, req *deliveryv1.GetApplicationRequest) (*deliveryv1.GetApplicationResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Applications.GetApplication(ctx, scope, req.GetApplicationId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.GetApplicationResponse{Application: toApplication(a)}, nil
}

// ListApplications lists an environment's applications.
func (s *Server) ListApplications(ctx context.Context, req *deliveryv1.ListApplicationsRequest) (*deliveryv1.ListApplicationsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.deps.Applications.ListApplications(ctx, scope, req.GetEnvironmentId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListApplicationsResponse{Applications: make([]*deliveryv1.Application, 0, len(list))}
	for i := range list {
		resp.Applications = append(resp.Applications, toApplication(&list[i]))
	}
	return resp, nil
}

// ListEndpoints lists an application's endpoints.
func (s *Server) ListEndpoints(ctx context.Context, req *deliveryv1.ListEndpointsRequest) (*deliveryv1.ListEndpointsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.deps.Endpoints.ListEndpoints(ctx, scope, req.GetApplicationId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListEndpointsResponse{Endpoints: make([]*deliveryv1.Endpoint, 0, len(list))}
	for i := range list {
		resp.Endpoints = append(resp.Endpoints, toEndpoint(&list[i]))
	}
	return resp, nil
}

// GetOverview reads an environment's overview tiles.
func (s *Server) GetOverview(ctx context.Context, req *deliveryv1.GetOverviewRequest) (*deliveryv1.GetOverviewResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	o, err := s.deps.Overview.Get(ctx, scope, req.GetEnvironmentId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.GetOverviewResponse{Overview: &deliveryv1.Overview{
		Since: timestamppb.New(o.Since), ComputedAt: timestamppb.New(o.ComputedAt), Accepted: o.Accepted, Succeeded: o.Succeeded, Exhausted: o.Exhausted, Backlog: o.Backlog,
		OldestPendingDueAt: optionalTime(o.OldestPendingDueAt), EndpointsHealthy: o.EndpointsHealthy, EndpointsUnhealthy: o.EndpointsUnhealthy, EndpointsUnknown: o.EndpointsUnknown, HealthObservedAt: optionalTime(o.HealthObservedAt),
	}}, nil
}

// SendTestMessage plans a test message for one endpoint.
func (s *Server) SendTestMessage(ctx context.Context, req *deliveryv1.SendTestMessageRequest) (*deliveryv1.SendTestMessageResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	m, d, err := s.deps.Messages.SendTest(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.SendTestMessageResponse{Message: toMessage(m, nil, 1), Delivery: toDelivery(d)}, nil
}

// UpdateApplication changes an application's settings.
func (s *Server) UpdateApplication(ctx context.Context, req *deliveryv1.UpdateApplicationRequest) (*deliveryv1.UpdateApplicationResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Applications.UpdateApplication(ctx, scope, req.GetApplicationId(), services.UpdateApplicationInput{ResponsePreviewsEnabled: req.ResponsePreviewsEnabled})
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.UpdateApplicationResponse{Application: toApplication(a)}, nil
}

// CreateEndpoint registers a destination and returns its signing secret once.
func (s *Server) CreateEndpoint(ctx context.Context, req *deliveryv1.CreateEndpointRequest) (*deliveryv1.CreateEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, secret, version, err := s.deps.Endpoints.CreateEndpoint(ctx, scope, services.CreateEndpointInput{
		ApplicationID: req.GetApplicationId(),
		URL:           req.GetUrl(),
		Description:   req.GetDescription(),
		EventTypes:    req.GetEventTypes(),
	})
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateEndpointResponse{Endpoint: toEndpoint(e), SigningSecret: secret, SecretVersion: version}, nil
}

// GetEndpoint reads one endpoint with its subscriptions and health.
func (s *Server) GetEndpoint(ctx context.Context, req *deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.deps.Endpoints.GetEndpoint(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.GetEndpointResponse{Endpoint: toEndpoint(e)}, nil
}

// GetWorkspaceLimits reports the quotas and the workspace's usage.
func (s *Server) GetWorkspaceLimits(ctx context.Context, _ *deliveryv1.GetWorkspaceLimitsRequest) (*deliveryv1.GetWorkspaceLimitsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.deps.Limits.Usage(ctx, scope)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.GetWorkspaceLimitsResponse{
		Limits:       &deliveryv1.WorkspaceLimits{EndpointsPerApplication: int32(u.EndpointsPerApplication), FanoutPerMessage: int32(u.FanoutPerMessage), BacklogPerWorkspace: int32(u.BacklogPerWorkspace)}, //nolint:gosec // configured limits
		Backlog:      int32(u.Backlog),                                                                                                                                                                       //nolint:gosec // bounded by the cap
		Applications: make([]*deliveryv1.ApplicationEndpointCount, 0, len(u.Applications)),
		Retention:    &deliveryv1.Retention{Payload: durationpb.New(u.Retention.Payload), IdempotencyWindow: durationpb.New(u.Retention.IdempotencyWindow), Audit: durationpb.New(u.Retention.Audit)},
	}
	for _, a := range u.Applications {
		resp.Applications = append(resp.Applications, &deliveryv1.ApplicationEndpointCount{ApplicationId: a.ApplicationID, Name: a.Name, Endpoints: a.Endpoints})
	}
	return resp, nil
}

// GetHealthEpisode reads one episode and the endpoint it belongs to.
func (s *Server) GetHealthEpisode(ctx context.Context, req *deliveryv1.GetHealthEpisodeRequest) (*deliveryv1.GetHealthEpisodeResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	episode, err := s.deps.Health.Episode(ctx, scope, req.GetEpisodeId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	endpoint, err := s.deps.Endpoints.GetEndpoint(ctx, scope, episode.EndpointID)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.GetHealthEpisodeResponse{
		Episode:  &deliveryv1.HealthEpisode{Id: episode.ID, EndpointId: episode.EndpointID, Revision: episode.Revision, ConsecutiveFailures: episode.ConsecutiveFailures, OpenedAt: timestamppb.New(episode.OpenedAt), ResolvedAt: optionalTime(episode.ResolvedAt)},
		Endpoint: toEndpoint(endpoint),
	}, nil
}

// PauseEndpoint stops new attempts for an endpoint.
func (s *Server) PauseEndpoint(ctx context.Context, req *deliveryv1.PauseEndpointRequest) (*deliveryv1.PauseEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.deps.Endpoints.PauseEndpoint(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.PauseEndpointResponse{Endpoint: toEndpoint(e)}, nil
}

// UpdateEndpoint changes the set fields and bumps the revision.
func (s *Server) UpdateEndpoint(ctx context.Context, req *deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	in := services.UpdateEndpointInput{EndpointID: req.GetEndpointId(), URL: req.Url, Description: req.Description, ExpectedRevision: req.ExpectedRevision}
	if req.EventTypes != nil {
		types := req.GetEventTypes().GetValues()
		if types == nil {
			types = []string{}
		}
		in.EventTypes = &types
	}
	e, err := s.deps.Endpoints.UpdateEndpoint(ctx, scope, in)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.UpdateEndpointResponse{Endpoint: toEndpoint(e)}, nil
}

// RotateEndpointSecret adds a signing secret and retires the previous ones after the overlap.
func (s *Server) RotateEndpointSecret(ctx context.Context, req *deliveryv1.RotateEndpointSecretRequest) (*deliveryv1.RotateEndpointSecretResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, secret, version, retireAt, err := s.deps.Endpoints.RotateEndpointSecret(ctx, scope, req.GetEndpointId(), req.GetRetireIn().AsDuration())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.RotateEndpointSecretResponse{Endpoint: toEndpoint(e), SigningSecret: secret, SecretVersion: version, RetireAt: timestamppb.New(retireAt)}, nil
}

// ResumeEndpoint lets a paused endpoint's backlog drain again.
func (s *Server) ResumeEndpoint(ctx context.Context, req *deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.deps.Endpoints.ResumeEndpoint(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.ResumeEndpointResponse{Endpoint: toEndpoint(e)}, nil
}

// ArchiveEndpoint retires an endpoint and cancels its unsent cycles.
func (s *Server) ArchiveEndpoint(ctx context.Context, req *deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, cancelled, err := s.deps.Endpoints.ArchiveEndpoint(ctx, scope, req.GetEndpointId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.ArchiveEndpointResponse{Endpoint: toEndpoint(e), CancelledDeliveries: cancelled}, nil
}

// ListAuditEvents pages the data plane's audit trail.
func (s *Server) ListAuditEvents(ctx context.Context, req *deliveryv1.ListAuditEventsRequest) (*deliveryv1.ListAuditEventsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	opts := repos.ListAuditOpts{ActorID: req.GetActorId(), ResourceType: req.GetResourceType(), ResourceID: req.GetResourceId(), Limit: int(req.GetLimit()), Offset: int(req.GetOffset())}
	if req.After != nil {
		t := req.GetAfter().AsTime()
		opts.After = &t
	}
	if req.Before != nil {
		t := req.GetBefore().AsTime()
		opts.Before = &t
	}
	items, total, err := s.deps.Audit.ListAuditEvents(ctx, scope, opts)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListAuditEventsResponse{Events: make([]*deliveryv1.AuditEvent, 0, len(items)), Total: int32(total)} //nolint:gosec // bounded by the page
	for i := range items {
		e := &items[i]
		resp.Events = append(resp.Events, &deliveryv1.AuditEvent{Id: e.ID, WorkspaceId: e.WorkspaceID, ActorId: e.ActorID, Action: e.Action, ResourceType: e.ResourceType, ResourceId: e.ResourceID, RequestId: e.RequestID, Details: e.Details, OccurredAt: timestamppb.New(e.OccurredAt)})
	}
	return resp, nil
}

func scope(ctx context.Context) (services.Scope, error) {
	sc, ok := grpcx.ScopeFromContext(ctx)
	if !ok {
		return services.Scope{}, status.Error(codes.Unauthenticated, "missing workspace")
	}
	return services.Scope{WorkspaceID: sc.WorkspaceID, ActorID: sc.ActorID}, nil
}

// CreateEnvironment adds an environment to the caller's workspace.
func (s *Server) CreateEnvironment(ctx context.Context, req *deliveryv1.CreateEnvironmentRequest) (*deliveryv1.CreateEnvironmentResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	e, err := s.deps.Environments.CreateEnvironment(ctx, scope, req.GetName(), req.GetKind())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateEnvironmentResponse{Environment: toEnvironment(e)}, nil
}

// ListEnvironments returns the caller's workspace's environments.
func (s *Server) ListEnvironments(ctx context.Context, _ *deliveryv1.ListEnvironmentsRequest) (*deliveryv1.ListEnvironmentsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.deps.Environments.ListEnvironments(ctx, scope)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListEnvironmentsResponse{Environments: make([]*deliveryv1.Environment, 0, len(list))}
	for i := range list {
		resp.Environments = append(resp.Environments, toEnvironment(&list[i]))
	}
	return resp, nil
}

func toEnvironment(e *models.Environment) *deliveryv1.Environment {
	return &deliveryv1.Environment{Id: e.ID, WorkspaceId: e.WorkspaceID, Name: e.Name, Kind: e.Kind, CreatedBy: e.CreatedBy, CreatedAt: timestamppb.New(e.CreatedAt)}
}

func toApplication(a *models.Application) *deliveryv1.Application {
	return &deliveryv1.Application{
		Id:              a.ID,
		WorkspaceId:     a.WorkspaceID,
		Name:            a.Name,
		CreatedBy:       a.CreatedBy,
		CreatedAt:       timestamppb.New(a.CreatedAt),
		ArchivedAt:      optionalTime(a.ArchivedAt),
		EnvironmentId:   a.EnvironmentID,
		EnvironmentKind: a.EnvironmentKind,

		ResponsePreviewsEnabled: a.ResponsePreviewsEnabled,
	}
}

func toEndpoint(e *models.Endpoint) *deliveryv1.Endpoint {
	return &deliveryv1.Endpoint{
		Id:            e.ID,
		ApplicationId: e.ApplicationID,
		Url:           e.URL,
		Description:   e.Description,
		Revision:      e.Revision,
		EventTypes:    e.EventTypes,
		PausedAt:      optionalTime(e.PausedAt),
		ArchivedAt:    optionalTime(e.ArchivedAt),
		CreatedAt:     timestamppb.New(e.CreatedAt),
		Health:        toHealth(e.Health),
		Waiting:       e.Waiting,
		Secrets:       toSecretInfos(e.Secrets),
	}
}

func toSecretInfos(in []models.EndpointSecretInfo) []*deliveryv1.EndpointSecretInfo {
	if in == nil {
		return nil
	}
	out := make([]*deliveryv1.EndpointSecretInfo, 0, len(in))
	for i := range in {
		out = append(out, &deliveryv1.EndpointSecretInfo{Version: in[i].Version, Fingerprint: in[i].Fingerprint, CreatedAt: timestamppb.New(in[i].CreatedAt), RetireAt: optionalTime(in[i].RetireAt)})
	}
	return out
}

func toHealth(h *models.HealthStatus) *deliveryv1.EndpointHealth {
	if h == nil {
		return nil
	}
	return &deliveryv1.EndpointHealth{State: h.State, ObservedAt: optionalTime(h.ObservedAt), Stale: h.Stale, ConsecutiveFailures: h.ConsecutiveFailures}
}

func optionalTime(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// AcceptMessage durably accepts an event and plans its deliveries.
func (s *Server) AcceptMessage(ctx context.Context, req *deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	res, err := s.deps.Messages.Accept(ctx, scope, services.AcceptInput{
		ApplicationID:  req.GetApplicationId(),
		EventType:      req.GetEventType(),
		IdempotencyKey: req.GetIdempotencyKey(),
		SenderEventID:  req.SenderEventId,
		Payload:        req.GetPayload(),
	})
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.AcceptMessageResponse{Message: toMessage(res.Message, nil, res.DeliveryCount), Duplicate: res.Duplicate}, nil
}

// GetMessage returns a message with its deliveries.
func (s *Server) GetMessage(ctx context.Context, req *deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	m, app, deliveries, err := s.deps.Messages.GetMessage(ctx, scope, req.GetMessageId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.GetMessageResponse{Message: toMessage(m, app, int32(len(deliveries))), Deliveries: make([]*deliveryv1.Delivery, 0, len(deliveries))} //nolint:gosec // bounded fan-out
	for i := range deliveries {
		resp.Deliveries = append(resp.Deliveries, toDelivery(&deliveries[i]))
	}
	return resp, nil
}

// GetMessagePayload returns the retained bytes and audits the read.
func (s *Server) GetMessagePayload(ctx context.Context, req *deliveryv1.GetMessagePayloadRequest) (*deliveryv1.GetMessagePayloadResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	m, err := s.deps.Messages.GetMessagePayload(ctx, scope, req.GetMessageId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.GetMessagePayloadResponse{Message: toMessage(m, nil, 0), Payload: m.Payload, PayloadExpiresAt: timestamppb.New(m.PayloadExpiresAt)}, nil
}

// ListMessages pages the explorer.
func (s *Server) ListMessages(ctx context.Context, req *deliveryv1.ListMessagesRequest) (*deliveryv1.ListMessagesResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	opts := repos.ListMessagesOpts{
		ApplicationID: req.GetApplicationId(), EnvironmentID: req.GetEnvironmentId(), EventType: req.GetEventType(), SenderEventID: req.GetSenderEventId(),
		Outcome: req.GetOutcome(), Limit: int(req.GetLimit()), Offset: int(req.GetOffset()), SortBy: req.GetSortBy(), SortDir: req.GetSortDir(),
	}
	if req.AcceptedAfter != nil {
		t := req.GetAcceptedAfter().AsTime()
		opts.AcceptedAfter = &t
	}
	if req.AcceptedBefore != nil {
		t := req.GetAcceptedBefore().AsTime()
		opts.AcceptedBefore = &t
	}
	items, total, err := s.deps.Messages.ListMessages(ctx, scope, opts)
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListMessagesResponse{Messages: make([]*deliveryv1.MessageSummary, 0, len(items)), Total: int32(total)} //nolint:gosec // bounded by the page
	for i := range items {
		m := &items[i]
		total := m.Queued + m.InFlight + m.RetryScheduled + m.Succeeded + m.Failed + m.Cancelled
		resp.Messages = append(resp.Messages, &deliveryv1.MessageSummary{
			Message:          &deliveryv1.Message{Id: m.ID, PublicId: m.PublicID, ApplicationId: m.ApplicationID, EventType: m.EventType, SenderEventId: m.SenderEventID, AcceptedAt: timestamppb.New(m.AcceptedAt), DeliveryCount: total, EnvironmentId: m.EnvironmentID, EnvironmentKind: m.EnvironmentKind},
			Counts:           &deliveryv1.DeliveryCounts{Queued: m.Queued, InFlight: m.InFlight, RetryScheduled: m.RetryScheduled, Succeeded: m.Succeeded, Failed: m.Failed, Cancelled: m.Cancelled},
			PayloadExpiresAt: timestamppb.New(m.PayloadExpiresAt),
		})
	}
	return resp, nil
}

// ListAttempts returns every attempt of a delivery, oldest first.
func (s *Server) ListAttempts(ctx context.Context, req *deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	attempts, err := s.deps.Messages.ListAttempts(ctx, scope, req.GetDeliveryId(), req.GetIncludePreviews())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.ListAttemptsResponse{Attempts: make([]*deliveryv1.Attempt, 0, len(attempts))}
	for i := range attempts {
		resp.Attempts = append(resp.Attempts, toAttempt(&attempts[i]))
	}
	return resp, nil
}

// CancelDelivery stops further attempts; a finished delivery comes back unchanged.
func (s *Server) CancelDelivery(ctx context.Context, req *deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.deps.Messages.CancelDelivery(ctx, scope, req.GetDeliveryId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CancelDeliveryResponse{Delivery: toDelivery(d)}, nil
}

// ReplayDelivery starts a new cycle for a failed or cancelled delivery.
func (s *Server) ReplayDelivery(ctx context.Context, req *deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	d, err := s.deps.Messages.ReplayDelivery(ctx, scope, req.GetDeliveryId(), req.GetIdempotencyKey())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.ReplayDeliveryResponse{Delivery: toDelivery(d)}, nil
}

// toMessage maps a message; app, when known, supplies the environment.
func toMessage(m *models.Message, app *models.Application, deliveryCount int32) *deliveryv1.Message {
	out := &deliveryv1.Message{
		Id:            m.ID,
		PublicId:      m.PublicID,
		ApplicationId: m.ApplicationID,
		EventType:     m.EventType,
		SenderEventId: m.SenderEventID,
		AcceptedAt:    timestamppb.New(m.AcceptedAt),
		DeliveryCount: deliveryCount,
	}
	if !m.PayloadExpiresAt.IsZero() {
		out.PayloadExpiresAt = timestamppb.New(m.PayloadExpiresAt)
	}
	if app != nil {
		out.EnvironmentId, out.EnvironmentKind = app.EnvironmentID, app.EnvironmentKind
	}
	return out
}

var deliveryStates = map[string]deliveryv1.DeliveryState{
	models.DeliveryQueued:         deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED,
	models.DeliveryInFlight:       deliveryv1.DeliveryState_DELIVERY_STATE_IN_FLIGHT,
	models.DeliveryRetryScheduled: deliveryv1.DeliveryState_DELIVERY_STATE_RETRY_SCHEDULED,
	models.DeliverySucceeded:      deliveryv1.DeliveryState_DELIVERY_STATE_SUCCEEDED,
	models.DeliveryFailed:         deliveryv1.DeliveryState_DELIVERY_STATE_FAILED,
	models.DeliveryCancelled:      deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED,
}

func toDelivery(d *models.Delivery) *deliveryv1.Delivery {
	return &deliveryv1.Delivery{
		Id:                 d.ID,
		MessageId:          d.MessageID,
		EndpointId:         d.EndpointID,
		State:              deliveryStates[d.State],
		AttemptCount:       d.AttemptCount,
		DueAt:              optionalTime(d.DueAt),
		DeadlineAt:         timestamppb.New(d.DeadlineAt),
		FailureReason:      d.FailureReason,
		ReplayOfDeliveryId: d.ReplayOfDeliveryID,
		CreatedAt:          timestamppb.New(d.CreatedAt),
		CompletedAt:        optionalTime(d.CompletedAt),
		EndpointRevision:   d.EndpointRevision,
	}
}

func toAttempt(a *models.Attempt) *deliveryv1.Attempt {
	return &deliveryv1.Attempt{
		Id:               a.ID,
		DeliveryId:       a.DeliveryID,
		Number:           a.Number,
		StartedAt:        timestamppb.New(a.StartedAt),
		CompletedAt:      optionalTime(a.CompletedAt),
		StatusCode:       a.StatusCode,
		ErrorClass:       a.ErrorClass,
		LatencyMs:        a.LatencyMS,
		SecretVersion:    a.SecretVersion,
		EndpointRevision: a.EndpointRevision,
		RequestUrl:       a.RequestURL,
		ResponsePreview:  a.ResponsePreview,
		ResponseHeaders:  a.ResponseHeaders,
	}
}
