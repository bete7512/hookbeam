package deliveryclient

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// client implements services.DataPlane over the generated gRPC client.
type client struct {
	rpc     deliveryv1.DeliveryServiceClient
	timeout time.Duration
}

var _ services.DataPlane = (*client)(nil)

// New returns the DataPlane over a generated client; every call is bounded by timeout.
func New(rpc deliveryv1.DeliveryServiceClient, timeout time.Duration) (services.DataPlane, error) {
	if rpc == nil || timeout <= 0 {
		return nil, errors.New("deliveryclient: client and positive timeout are required")
	}
	return &client{rpc: rpc, timeout: timeout}, nil
}

// call scopes ctx to the workspace and actor and bounds it.
func (c *client) call(ctx context.Context, scope services.Scope) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	return grpcx.WithScope(ctx, grpcx.Scope{WorkspaceID: scope.WorkspaceID, ActorID: scope.ActorID}), cancel
}

func (c *client) CreateApplication(ctx context.Context, scope services.Scope, environmentID *int64, name string) (*models.Application, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.CreateApplication(ctx, &deliveryv1.CreateApplicationRequest{Name: name, EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (c *client) GetApplication(ctx context.Context, scope services.Scope, id int64) (*models.Application, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetApplication(ctx, &deliveryv1.GetApplicationRequest{ApplicationId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (c *client) ListApplications(ctx context.Context, scope services.Scope, environmentID int64) ([]models.Application, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ListApplications(ctx, &deliveryv1.ListApplicationsRequest{EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Application, 0, len(resp.GetApplications()))
	for _, a := range resp.GetApplications() {
		out = append(out, *toApplication(a))
	}
	return out, nil
}

func (c *client) UpdateApplication(ctx context.Context, scope services.Scope, id int64, in services.UpdateApplicationInput) (*models.Application, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.UpdateApplication(ctx, &deliveryv1.UpdateApplicationRequest{ApplicationId: id, ResponsePreviewsEnabled: in.ResponsePreviewsEnabled})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), nil
}

func (c *client) ArchiveApplication(ctx context.Context, scope services.Scope, id int64) (*models.Application, int64, int64, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ArchiveApplication(ctx, &deliveryv1.ArchiveApplicationRequest{ApplicationId: id})
	if err != nil {
		return nil, 0, 0, grpcx.FromStatus(err)
	}
	return toApplication(resp.GetApplication()), resp.GetArchivedEndpoints(), resp.GetCancelledDeliveries(), nil
}

func (c *client) ListEndpoints(ctx context.Context, scope services.Scope, applicationID int64) ([]models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ListEndpoints(ctx, &deliveryv1.ListEndpointsRequest{ApplicationId: applicationID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Endpoint, 0, len(resp.GetEndpoints()))
	for _, e := range resp.GetEndpoints() {
		out = append(out, *toEndpoint(e))
	}
	return out, nil
}

func (c *client) GetOverview(ctx context.Context, scope services.Scope, environmentID int64) (*models.Overview, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetOverview(ctx, &deliveryv1.GetOverviewRequest{EnvironmentId: environmentID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toOverview(resp.GetOverview()), nil
}

func (c *client) SendTestMessage(ctx context.Context, scope services.Scope, endpointID int64) (*models.Message, *models.Delivery, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.SendTestMessage(ctx, &deliveryv1.SendTestMessageRequest{EndpointId: endpointID})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return toMessage(resp.GetMessage()), &d, nil
}

func (c *client) CreateEnvironment(ctx context.Context, scope services.Scope, name, kind string) (*models.Environment, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.CreateEnvironment(ctx, &deliveryv1.CreateEnvironmentRequest{Name: name, Kind: kind})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEnvironment(resp.GetEnvironment()), nil
}

func (c *client) ListEnvironments(ctx context.Context, scope services.Scope) ([]models.Environment, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ListEnvironments(ctx, &deliveryv1.ListEnvironmentsRequest{})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	out := make([]models.Environment, 0, len(resp.GetEnvironments()))
	for _, e := range resp.GetEnvironments() {
		out = append(out, *toEnvironment(e))
	}
	return out, nil
}

func (c *client) CreateEndpoint(ctx context.Context, scope services.Scope, in services.CreateEndpointInput) (*models.Endpoint, string, int32, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.CreateEndpoint(ctx, &deliveryv1.CreateEndpointRequest{ApplicationId: in.ApplicationID, Url: in.URL, Description: in.Description, EventTypes: in.EventTypes})
	if err != nil {
		return nil, "", 0, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), resp.GetSigningSecret(), resp.GetSecretVersion(), nil
}

func (c *client) GetEndpoint(ctx context.Context, scope services.Scope, id int64) (*models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetEndpoint(ctx, &deliveryv1.GetEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (c *client) GetWorkspaceLimits(ctx context.Context, scope services.Scope) (*models.WorkspaceLimits, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetWorkspaceLimits(ctx, &deliveryv1.GetWorkspaceLimitsRequest{})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toWorkspaceLimits(resp), nil
}

func (c *client) GetHealthEpisode(ctx context.Context, scope services.Scope, id int64) (*models.HealthEpisode, *models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetHealthEpisode(ctx, &deliveryv1.GetHealthEpisodeRequest{EpisodeId: id})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	return toHealthEpisode(resp.GetEpisode()), toEndpoint(resp.GetEndpoint()), nil
}

func (c *client) PauseEndpoint(ctx context.Context, scope services.Scope, id int64) (*models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.PauseEndpoint(ctx, &deliveryv1.PauseEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (c *client) ResumeEndpoint(ctx context.Context, scope services.Scope, id int64) (*models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ResumeEndpoint(ctx, &deliveryv1.ResumeEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (c *client) UpdateEndpoint(ctx context.Context, scope services.Scope, in services.UpdateEndpointInput) (*models.Endpoint, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	req := &deliveryv1.UpdateEndpointRequest{EndpointId: in.EndpointID, Url: in.URL, Description: in.Description, ExpectedRevision: in.ExpectedRevision}
	if in.EventTypes != nil {
		req.EventTypes = &deliveryv1.EventTypeList{Values: *in.EventTypes}
	}
	resp, err := c.rpc.UpdateEndpoint(ctx, req)
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), nil
}

func (c *client) RotateEndpointSecret(ctx context.Context, scope services.Scope, id int64, retireIn time.Duration) (*models.SecretRotated, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.RotateEndpointSecret(ctx, &deliveryv1.RotateEndpointSecretRequest{EndpointId: id, RetireIn: durationpb.New(retireIn)})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.SecretRotated{Endpoint: toEndpoint(resp.GetEndpoint()), SigningSecret: resp.GetSigningSecret(), SecretVersion: resp.GetSecretVersion(), RetireAt: resp.GetRetireAt().AsTime()}, nil
}

func (c *client) ArchiveEndpoint(ctx context.Context, scope services.Scope, id int64) (*models.Endpoint, int64, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ArchiveEndpoint(ctx, &deliveryv1.ArchiveEndpointRequest{EndpointId: id})
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	return toEndpoint(resp.GetEndpoint()), resp.GetCancelledDeliveries(), nil
}

func (c *client) AcceptMessage(ctx context.Context, scope services.Scope, in services.AcceptMessageInput) (*models.MessageAccepted, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.AcceptMessage(ctx, &deliveryv1.AcceptMessageRequest{
		ApplicationId:  in.ApplicationID,
		EventType:      in.EventType,
		IdempotencyKey: in.IdempotencyKey,
		SenderEventId:  in.SenderEventID,
		Payload:        in.Payload,
	})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.MessageAccepted{Message: *toMessage(resp.GetMessage()), Duplicate: resp.GetDuplicate()}, nil
}

func (c *client) GetMessage(ctx context.Context, scope services.Scope, id int64) (*models.Message, []models.Delivery, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetMessage(ctx, &deliveryv1.GetMessageRequest{MessageId: id})
	if err != nil {
		return nil, nil, grpcx.FromStatus(err)
	}
	deliveries := make([]models.Delivery, 0, len(resp.GetDeliveries()))
	for _, d := range resp.GetDeliveries() {
		deliveries = append(deliveries, toDelivery(d))
	}
	return toMessage(resp.GetMessage()), deliveries, nil
}

func (c *client) GetMessagePayload(ctx context.Context, scope services.Scope, id int64) (*models.MessagePayload, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.GetMessagePayload(ctx, &deliveryv1.GetMessagePayloadRequest{MessageId: id})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	return &models.MessagePayload{Message: *toMessage(resp.GetMessage()), Body: resp.GetPayload(), ExpiresAt: resp.GetPayloadExpiresAt().AsTime()}, nil
}

func (c *client) ListMessages(ctx context.Context, scope services.Scope, in services.ListMessagesInput) ([]models.MessageSummary, int, error) {
	ctx, cancel := c.call(ctx, scope)
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
	resp, err := c.rpc.ListMessages(ctx, req)
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	items := make([]models.MessageSummary, 0, len(resp.GetMessages()))
	for _, m := range resp.GetMessages() {
		items = append(items, toMessageSummary(m))
	}
	return items, int(resp.GetTotal()), nil
}

func (c *client) ListAuditEvents(ctx context.Context, scope services.Scope, in services.ListAuditInput) ([]models.AuditEvent, int, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	req := &deliveryv1.ListAuditEventsRequest{ActorId: in.ActorID, ResourceType: in.ResourceType, ResourceId: in.ResourceID, Limit: int32(in.Limit), Offset: int32(in.Offset)} //nolint:gosec // validated page sizes
	if in.After != nil {
		req.After = timestamppb.New(*in.After)
	}
	if in.Before != nil {
		req.Before = timestamppb.New(*in.Before)
	}
	resp, err := c.rpc.ListAuditEvents(ctx, req)
	if err != nil {
		return nil, 0, grpcx.FromStatus(err)
	}
	out := make([]models.AuditEvent, 0, len(resp.GetEvents()))
	for _, e := range resp.GetEvents() {
		out = append(out, toAuditEvent(e))
	}
	return out, int(resp.GetTotal()), nil
}

func (c *client) ListAttempts(ctx context.Context, scope services.Scope, deliveryID int64, includePreviews bool) ([]models.Attempt, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ListAttempts(ctx, &deliveryv1.ListAttemptsRequest{DeliveryId: deliveryID, IncludePreviews: includePreviews})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	attempts := make([]models.Attempt, 0, len(resp.GetAttempts()))
	for _, a := range resp.GetAttempts() {
		attempts = append(attempts, toAttempt(a))
	}
	return attempts, nil
}

func (c *client) CancelDelivery(ctx context.Context, scope services.Scope, deliveryID int64) (*models.Delivery, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.CancelDelivery(ctx, &deliveryv1.CancelDeliveryRequest{DeliveryId: deliveryID})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return &d, nil
}

func (c *client) ReplayDelivery(ctx context.Context, scope services.Scope, deliveryID int64, idempotencyKey string) (*models.Delivery, error) {
	ctx, cancel := c.call(ctx, scope)
	defer cancel()
	resp, err := c.rpc.ReplayDelivery(ctx, &deliveryv1.ReplayDeliveryRequest{DeliveryId: deliveryID, IdempotencyKey: idempotencyKey})
	if err != nil {
		return nil, grpcx.FromStatus(err)
	}
	d := toDelivery(resp.GetDelivery())
	return &d, nil
}
