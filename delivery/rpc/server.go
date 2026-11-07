// Package rpc implements the DeliveryService gRPC contract over the services; it is imported only by cmd/server.
package rpc

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// Deps are the server dependencies; all are required.
type Deps struct {
	Applications services.ApplicationService
	Endpoints    services.EndpointService
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
	if deps.Applications == nil || deps.Endpoints == nil || deps.Messages == nil || deps.Logger == nil {
		return nil, errors.New("rpc: Applications, Endpoints, Messages and Logger are required")
	}
	return &Server{deps: deps}, nil
}

// CreateApplication registers a consumer application in the caller's workspace.
func (s *Server) CreateApplication(ctx context.Context, req *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.deps.Applications.CreateApplication(ctx, scope, req.GetName())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	return &deliveryv1.CreateApplicationResponse{Application: toApplication(a)}, nil
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
	in := services.UpdateEndpointInput{EndpointID: req.GetEndpointId(), URL: req.Url, Description: req.Description}
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

func scope(ctx context.Context) (services.Scope, error) {
	sc, ok := grpcx.ScopeFromContext(ctx)
	if !ok {
		return services.Scope{}, status.Error(codes.Unauthenticated, "missing workspace")
	}
	return services.Scope{WorkspaceID: sc.WorkspaceID, ActorID: sc.ActorID}, nil
}

func toApplication(a *models.Application) *deliveryv1.Application {
	return &deliveryv1.Application{
		Id:          a.ID,
		WorkspaceId: a.WorkspaceID,
		Name:        a.Name,
		CreatedBy:   a.CreatedBy,
		CreatedAt:   timestamppb.New(a.CreatedAt),
		ArchivedAt:  optionalTime(a.ArchivedAt),
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
	}
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
	return &deliveryv1.AcceptMessageResponse{Message: toMessage(res.Message, res.DeliveryCount), Duplicate: res.Duplicate}, nil
}

// GetMessage returns a message with its deliveries.
func (s *Server) GetMessage(ctx context.Context, req *deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	m, deliveries, err := s.deps.Messages.GetMessage(ctx, scope, req.GetMessageId())
	if err != nil {
		return nil, grpcx.Status(err)
	}
	resp := &deliveryv1.GetMessageResponse{Message: toMessage(m, int32(len(deliveries))), Deliveries: make([]*deliveryv1.Delivery, 0, len(deliveries))} //nolint:gosec // bounded fan-out
	for i := range deliveries {
		resp.Deliveries = append(resp.Deliveries, toDelivery(&deliveries[i]))
	}
	return resp, nil
}

// ListAttempts returns every attempt of a delivery, oldest first.
func (s *Server) ListAttempts(ctx context.Context, req *deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error) {
	scope, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	attempts, err := s.deps.Messages.ListAttempts(ctx, scope, req.GetDeliveryId())
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

func toMessage(m *models.Message, deliveryCount int32) *deliveryv1.Message {
	return &deliveryv1.Message{
		Id:            m.ID,
		PublicId:      m.PublicID,
		ApplicationId: m.ApplicationID,
		EventType:     m.EventType,
		SenderEventId: m.SenderEventID,
		AcceptedAt:    timestamppb.New(m.AcceptedAt),
		DeliveryCount: deliveryCount,
	}
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
	}
}
