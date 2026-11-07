package services

//go:generate mockgen -source=delivery.go -destination=service_mocks/mock_delivery.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

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

// SubmitMessageInput is a raw submission; Payload is forwarded byte for byte.
type SubmitMessageInput struct {
	ApplicationID  int64
	IdempotencyKey string
	Payload        []byte
}

// DeliveryService is the control plane's door to the data plane: it scopes every call to the caller's
// workspace, forwards it over gRPC and maps the answer back into local models and errors.
type DeliveryService interface {
	CreateApplication(ctx context.Context, actor Actor, name string) (*models.Application, error)
	// CreateEndpoint returns the endpoint and its signing secret, shown once.
	CreateEndpoint(ctx context.Context, actor Actor, in CreateEndpointInput) (endpoint *models.Endpoint, secret string, version int32, err error)
	PauseEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error)
	ResumeEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error)
	// ArchiveEndpoint retires an endpoint; cancelled is how many unsent deliveries it stopped.
	ArchiveEndpoint(ctx context.Context, actor Actor, id int64) (endpoint *models.Endpoint, cancelled int64, err error)
	// SubmitMessage reads the event type from the payload's "type" and accepts the event for the workspace's application.
	SubmitMessage(ctx context.Context, workspaceID string, in SubmitMessageInput) (*models.MessageAccepted, error)
	GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, []models.Delivery, error)
	ListAttempts(ctx context.Context, workspaceID string, deliveryID int64) ([]models.Attempt, error)
	// CancelDelivery stops further attempts of a delivery; repeating it returns the same finished delivery.
	CancelDelivery(ctx context.Context, actor Actor, deliveryID int64) (*models.Delivery, error)
	// ReplayDelivery starts a new cycle for a failed or cancelled delivery; the same key returns the same cycle.
	ReplayDelivery(ctx context.Context, actor Actor, deliveryID int64, idempotencyKey string) (*models.Delivery, error)
}

// DeliveryServiceDeps are the dependencies; all are required.
type DeliveryServiceDeps struct {
	Client  deliveryv1.DeliveryServiceClient
	Timeout time.Duration
	Logger  *slog.Logger
}

type deliveryService struct {
	deps DeliveryServiceDeps
}

// NewDeliveryService returns a DeliveryService or an error if a dependency is missing.
func NewDeliveryService(deps DeliveryServiceDeps) (DeliveryService, error) {
	if deps.Client == nil || deps.Timeout <= 0 || deps.Logger == nil {
		return nil, errors.New("services: Client, positive Timeout and Logger are required")
	}
	return &deliveryService{deps: deps}, nil
}

// call scopes ctx to the workspace and actor and bounds it.
func (s *deliveryService) call(ctx context.Context, workspaceID, actorID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, s.deps.Timeout)
	return grpcx.WithScope(ctx, grpcx.Scope{WorkspaceID: workspaceID, ActorID: actorID}), cancel
}

func (s *deliveryService) CreateApplication(ctx context.Context, actor Actor, name string) (*models.Application, error) {
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CreateApplication(ctx, &deliveryv1.CreateApplicationRequest{Name: name})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (s *deliveryService) CreateEndpoint(ctx context.Context, actor Actor, in CreateEndpointInput) (*models.Endpoint, string, int32, error) {
	if actor.WorkspaceID == "" {
		return nil, "", 0, apperr.NewForbidden("select a workspace", nil)
	}
	ctx, cancel := s.call(ctx, actor.WorkspaceID, actor.UserID)
	defer cancel()
	resp, err := s.deps.Client.CreateEndpoint(ctx, &deliveryv1.CreateEndpointRequest{ApplicationId: in.ApplicationID, Url: in.URL, Description: in.Description, EventTypes: in.EventTypes})
	if err != nil {
		return nil, "", 0, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), resp.GetSigningSecret(), resp.GetSecretVersion(), nil
}

func (s *deliveryService) PauseEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
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
func (s *deliveryService) ResumeEndpoint(ctx context.Context, actor Actor, id int64) (*models.Endpoint, error) {
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
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
	if actor.WorkspaceID == "" {
		return nil, 0, apperr.NewForbidden("select a workspace", nil)
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

func (s *deliveryService) ListAttempts(ctx context.Context, workspaceID string, deliveryID int64) ([]models.Attempt, error) {
	if workspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	ctx, cancel := s.call(ctx, workspaceID, "")
	defer cancel()
	resp, err := s.deps.Client.ListAttempts(ctx, &deliveryv1.ListAttemptsRequest{DeliveryId: deliveryID})
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
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
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
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
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
	return &models.Application{ID: a.GetId(), WorkspaceID: a.GetWorkspaceId(), Name: a.GetName(), CreatedBy: a.GetCreatedBy(), CreatedAt: a.GetCreatedAt().AsTime(), ArchivedAt: optionalTime(a.ArchivedAt)}
}

func toEndpoint(e *deliveryv1.Endpoint) *models.Endpoint {
	types := e.GetEventTypes()
	if types == nil {
		types = []string{}
	}
	return &models.Endpoint{
		ID: e.GetId(), ApplicationID: e.GetApplicationId(), URL: e.GetUrl(), Description: e.GetDescription(), Revision: e.GetRevision(),
		EventTypes: types, PausedAt: optionalTime(e.PausedAt), ArchivedAt: optionalTime(e.ArchivedAt), CreatedAt: e.GetCreatedAt().AsTime(),
	}
}

func toMessage(m *deliveryv1.Message) *models.Message {
	return &models.Message{ID: m.GetId(), WebhookID: m.GetPublicId(), ApplicationID: m.GetApplicationId(), Type: m.GetEventType(), ExternalID: m.SenderEventId, AcceptedAt: m.GetAcceptedAt().AsTime(), DeliveryCount: m.GetDeliveryCount()}
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
	}
}

func optionalTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := t.AsTime()
	return &v
}
