package services_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// viewer is a user the fixture's authorizer refuses the payload capability.
const viewer = "u_viewer"

// developer is a user the fixture's authorizer allows everything.
var developer = services.Actor{UserID: "u_ada", WorkspaceID: ws}

// fakeClient answers the contract from function fields and records the outgoing metadata.
type fakeClient struct {
	deliveryv1.DeliveryServiceClient
	md        metadata.MD
	accept    func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error)
	create    func(*deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error)
	updateApp func(*deliveryv1.UpdateApplicationRequest) (*deliveryv1.UpdateApplicationResponse, error)
	getEp     func(*deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error)
	payload   func(*deliveryv1.GetMessagePayloadRequest) (*deliveryv1.GetMessagePayloadResponse, error)
	attempts  func(*deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error)
	createEnv func(*deliveryv1.CreateEnvironmentRequest) (*deliveryv1.CreateEnvironmentResponse, error)
	listEnvs  func(*deliveryv1.ListEnvironmentsRequest) (*deliveryv1.ListEnvironmentsResponse, error)
	get       func(*deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error)
	list      func(*deliveryv1.ListMessagesRequest) (*deliveryv1.ListMessagesResponse, error)
	audit     func(*deliveryv1.ListAuditEventsRequest) (*deliveryv1.ListAuditEventsResponse, error)
	cancel    func(*deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error)
	replay    func(*deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error)
	update    func(*deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error)
	rotate    func(*deliveryv1.RotateEndpointSecretRequest) (*deliveryv1.RotateEndpointSecretResponse, error)
	resume    func(*deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error)
	archive   func(*deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error)
}

func (f *fakeClient) UpdateEndpoint(ctx context.Context, in *deliveryv1.UpdateEndpointRequest, _ ...grpc.CallOption) (*deliveryv1.UpdateEndpointResponse, error) {
	f.record(ctx)
	return f.update(in)
}

func (f *fakeClient) RotateEndpointSecret(ctx context.Context, in *deliveryv1.RotateEndpointSecretRequest, _ ...grpc.CallOption) (*deliveryv1.RotateEndpointSecretResponse, error) {
	f.record(ctx)
	return f.rotate(in)
}

func (f *fakeClient) ResumeEndpoint(ctx context.Context, in *deliveryv1.ResumeEndpointRequest, _ ...grpc.CallOption) (*deliveryv1.ResumeEndpointResponse, error) {
	f.record(ctx)
	return f.resume(in)
}

func (f *fakeClient) ArchiveEndpoint(ctx context.Context, in *deliveryv1.ArchiveEndpointRequest, _ ...grpc.CallOption) (*deliveryv1.ArchiveEndpointResponse, error) {
	f.record(ctx)
	return f.archive(in)
}

func (f *fakeClient) ReplayDelivery(ctx context.Context, in *deliveryv1.ReplayDeliveryRequest, _ ...grpc.CallOption) (*deliveryv1.ReplayDeliveryResponse, error) {
	f.record(ctx)
	return f.replay(in)
}

func (f *fakeClient) CancelDelivery(ctx context.Context, in *deliveryv1.CancelDeliveryRequest, _ ...grpc.CallOption) (*deliveryv1.CancelDeliveryResponse, error) {
	f.record(ctx)
	return f.cancel(in)
}

func (f *fakeClient) record(ctx context.Context) {
	f.md, _ = metadata.FromOutgoingContext(ctx)
	if _, ok := ctx.Deadline(); !ok {
		panic("call without a deadline")
	}
}

func (f *fakeClient) ListAuditEvents(ctx context.Context, in *deliveryv1.ListAuditEventsRequest, _ ...grpc.CallOption) (*deliveryv1.ListAuditEventsResponse, error) {
	f.record(ctx)
	return f.audit(in)
}

func (f *fakeClient) ListMessages(ctx context.Context, in *deliveryv1.ListMessagesRequest, _ ...grpc.CallOption) (*deliveryv1.ListMessagesResponse, error) {
	f.record(ctx)
	return f.list(in)
}

func (f *fakeClient) CreateEnvironment(ctx context.Context, in *deliveryv1.CreateEnvironmentRequest, _ ...grpc.CallOption) (*deliveryv1.CreateEnvironmentResponse, error) {
	f.record(ctx)
	return f.createEnv(in)
}

func (f *fakeClient) ListEnvironments(ctx context.Context, in *deliveryv1.ListEnvironmentsRequest, _ ...grpc.CallOption) (*deliveryv1.ListEnvironmentsResponse, error) {
	f.record(ctx)
	return f.listEnvs(in)
}

func (f *fakeClient) CreateApplication(ctx context.Context, in *deliveryv1.CreateApplicationRequest, _ ...grpc.CallOption) (*deliveryv1.CreateApplicationResponse, error) {
	f.record(ctx)
	return f.create(in)
}

func (f *fakeClient) AcceptMessage(ctx context.Context, in *deliveryv1.AcceptMessageRequest, _ ...grpc.CallOption) (*deliveryv1.AcceptMessageResponse, error) {
	f.record(ctx)
	return f.accept(in)
}

func (f *fakeClient) GetEndpoint(ctx context.Context, in *deliveryv1.GetEndpointRequest, _ ...grpc.CallOption) (*deliveryv1.GetEndpointResponse, error) {
	f.record(ctx)
	return f.getEp(in)
}

func (f *fakeClient) UpdateApplication(ctx context.Context, in *deliveryv1.UpdateApplicationRequest, _ ...grpc.CallOption) (*deliveryv1.UpdateApplicationResponse, error) {
	f.record(ctx)
	return f.updateApp(in)
}

func (f *fakeClient) GetMessagePayload(ctx context.Context, in *deliveryv1.GetMessagePayloadRequest, _ ...grpc.CallOption) (*deliveryv1.GetMessagePayloadResponse, error) {
	f.record(ctx)
	return f.payload(in)
}

func (f *fakeClient) ListAttempts(ctx context.Context, in *deliveryv1.ListAttemptsRequest, _ ...grpc.CallOption) (*deliveryv1.ListAttemptsResponse, error) {
	f.record(ctx)
	return f.attempts(in)
}

func (f *fakeClient) GetMessage(ctx context.Context, in *deliveryv1.GetMessageRequest, _ ...grpc.CallOption) (*deliveryv1.GetMessageResponse, error) {
	f.record(ctx)
	return f.get(in)
}

func newDelivery(t *testing.T) (services.DeliveryService, *fakeClient) {
	t.Helper()
	client := &fakeClient{}
	authz := service_mocks.NewMockAuthorizer(gomock.NewController(t))
	authz.EXPECT().Can(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a services.Actor, c services.Capability) error {
		if a.WorkspaceID == "" {
			return apperr.NewForbidden("select a workspace", nil)
		}
		if a.UserID == viewer && c == services.CapReadPayloads {
			return apperr.NewForbidden("a viewer cannot read payloads", nil)
		}
		return nil
	}).AnyTimes()
	svc, err := services.NewDeliveryService(services.DeliveryServiceDeps{Client: client, Authz: authz, Timeout: time.Second, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, client
}

func TestNewDeliveryService(t *testing.T) {
	_, err := services.NewDeliveryService(services.DeliveryServiceDeps{})
	require.Error(t, err)
}

func TestDeliveryCreateApplication(t *testing.T) {
	svc, client := newDelivery(t)
	now := time.Now().UTC().Truncate(time.Second)
	client.create = func(in *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
		require.Equal(t, "Customer A", in.GetName())
		return &deliveryv1.CreateApplicationResponse{Application: &deliveryv1.Application{Id: 1, WorkspaceId: ws, Name: in.GetName(), CreatedBy: owner, CreatedAt: timestamppb.New(now)}}, nil
	}
	app, err := svc.CreateApplication(context.Background(), services.Actor{UserID: owner, WorkspaceID: ws}, "Customer A")
	require.NoError(t, err)
	require.Equal(t, int64(1), app.ID)
	require.Equal(t, now, app.CreatedAt)
	require.Nil(t, app.ArchivedAt)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID), "workspace travels as metadata")
	require.Equal(t, []string{owner}, client.md.Get(grpcx.MetadataActorID))

	client.create = func(*deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
		return nil, status.Error(codes.AlreadyExists, "exists")
	}
	_, err = svc.CreateApplication(context.Background(), services.Actor{UserID: owner, WorkspaceID: ws}, "Customer A")
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.Code)

	_, err = svc.CreateApplication(context.Background(), services.Actor{UserID: owner}, "Customer A")
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 403, appErr.Code)
}

func TestDeliverySubmitMessage(t *testing.T) {
	svc, client := newDelivery(t)
	payload := []byte(`{"type":"order.shipped","external_id":"order-817","data":{"n":1}}`)
	client.accept = func(in *deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
		require.Equal(t, int64(7), in.GetApplicationId())
		require.Equal(t, "order.shipped", in.GetEventType())
		require.Equal(t, "order-817", in.GetSenderEventId())
		require.Equal(t, "k1", in.GetIdempotencyKey())
		require.Equal(t, payload, in.GetPayload(), "bytes are forwarded untouched")
		return &deliveryv1.AcceptMessageResponse{Message: &deliveryv1.Message{Id: 42, PublicId: "msg_x", ApplicationId: 7, EventType: "order.shipped", DeliveryCount: 2, AcceptedAt: timestamppb.Now()}, Duplicate: true}, nil
	}
	res, err := svc.SubmitMessage(context.Background(), ws, services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "k1", Payload: payload})
	require.NoError(t, err)
	require.Equal(t, int64(42), res.Message.ID)
	require.Equal(t, "msg_x", res.Message.WebhookID)
	require.True(t, res.Duplicate)
	require.Empty(t, client.md.Get(grpcx.MetadataActorID), "a key has no acting user")

	var appErr *apperr.AppError
	_, err = svc.SubmitMessage(context.Background(), ws, services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "k1", Payload: []byte(`{"data":{}}`)})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 422, appErr.Code)
	require.Contains(t, appErr.Fields, "body")

	_, err = svc.SubmitMessage(context.Background(), ws, services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "k1", Payload: []byte(`not json`)})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 422, appErr.Code)

	_, err = svc.SubmitMessage(context.Background(), "", services.SubmitMessageInput{Payload: payload})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 401, appErr.Code)

	client.accept = func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	_, err = svc.SubmitMessage(context.Background(), ws, services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "k1", Payload: payload})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 503, appErr.Code)
}

func TestDeliveryGetMessage(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	reason := "http_503"
	client.get = func(in *deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error) {
		if in.GetMessageId() != 42 {
			return nil, status.Error(codes.NotFound, "message not found")
		}
		return &deliveryv1.GetMessageResponse{
			Message:    &deliveryv1.Message{Id: 42, PublicId: "msg_x", AcceptedAt: now, DeliveryCount: 1},
			Deliveries: []*deliveryv1.Delivery{{Id: 5, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_RETRY_SCHEDULED, AttemptCount: 1, DueAt: now, DeadlineAt: now, CreatedAt: now, FailureReason: &reason}},
		}, nil
	}
	m, ds, err := svc.GetMessage(context.Background(), ws, 42)
	require.NoError(t, err)
	require.Equal(t, "msg_x", m.WebhookID)
	require.Len(t, ds, 1)
	require.Equal(t, "retry_scheduled", ds[0].State)
	require.Equal(t, "http_503", *ds[0].FailureReason)
	require.NotNil(t, ds[0].DueAt)
	require.Nil(t, ds[0].CompletedAt)

	_, _, err = svc.GetMessage(context.Background(), ws, -1)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.Code)
	require.Equal(t, "message not found", appErr.Message)
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestDeliveryCancelDelivery(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	reason := "cancelled"
	client.cancel = func(in *deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error) {
		if in.GetDeliveryId() != 5 {
			return nil, status.Error(codes.NotFound, "delivery not found")
		}
		return &deliveryv1.CancelDeliveryResponse{Delivery: &deliveryv1.Delivery{Id: 5, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED, AttemptCount: 1, DeadlineAt: now, CreatedAt: now, CompletedAt: now, FailureReason: &reason}}, nil
	}
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}

	d, err := svc.CancelDelivery(context.Background(), actor, 5)
	require.NoError(t, err)
	require.Equal(t, "cancelled", d.State)
	require.Equal(t, "cancelled", *d.FailureReason)
	require.NotNil(t, d.CompletedAt)
	require.Equal(t, []string{ws}, client.md.Get("x-hookbeam-workspace-id"))
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	_, err = svc.CancelDelivery(context.Background(), actor, -1)
	require.ErrorIs(t, err, apperr.ErrNotFound)

	_, err = svc.CancelDelivery(context.Background(), services.Actor{UserID: "u_ada"}, 5)
	require.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestDeliveryReplayDelivery(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	original := int64(5)
	client.replay = func(in *deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error) {
		if in.GetIdempotencyKey() != "r1" {
			return nil, status.Error(codes.AlreadyExists, "an active delivery cycle already exists for this endpoint")
		}
		return &deliveryv1.ReplayDeliveryResponse{Delivery: &deliveryv1.Delivery{Id: 9, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED, DueAt: now, DeadlineAt: now, CreatedAt: now, ReplayOfDeliveryId: &original}}, nil
	}
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}

	d, err := svc.ReplayDelivery(context.Background(), actor, 5, "r1")
	require.NoError(t, err)
	require.Equal(t, int64(9), d.ID)
	require.Equal(t, int64(5), *d.ReplayOfDeliveryID)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	_, err = svc.ReplayDelivery(context.Background(), actor, 5, "r2")
	require.ErrorIs(t, err, apperr.ErrConflict)

	_, err = svc.ReplayDelivery(context.Background(), services.Actor{UserID: "u_ada"}, 5, "r1")
	require.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestDeliveryResumeAndArchiveEndpoint(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}
	client.resume = func(in *deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error) {
		if in.GetEndpointId() != 3 {
			return nil, status.Error(codes.AlreadyExists, "endpoint is archived")
		}
		return &deliveryv1.ResumeEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, ApplicationId: 7, Url: "https://r", Revision: 1, CreatedAt: now}}, nil
	}
	client.archive = func(in *deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error) {
		return &deliveryv1.ArchiveEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: in.GetEndpointId(), ApplicationId: 7, Url: "https://r", Revision: 1, ArchivedAt: now, CreatedAt: now}, CancelledDeliveries: 2}, nil
	}

	e, err := svc.ResumeEndpoint(context.Background(), actor, 3)
	require.NoError(t, err)
	require.Nil(t, e.PausedAt)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))
	_, err = svc.ResumeEndpoint(context.Background(), actor, 4)
	require.ErrorIs(t, err, apperr.ErrConflict)

	e, cancelled, err := svc.ArchiveEndpoint(context.Background(), actor, 3)
	require.NoError(t, err)
	require.NotNil(t, e.ArchivedAt)
	require.Equal(t, int64(2), cancelled)

	_, _, err = svc.ArchiveEndpoint(context.Background(), services.Actor{UserID: "u_ada"}, 3)
	require.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestDeliveryUpdateEndpoint(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}
	var got *deliveryv1.UpdateEndpointRequest
	client.update = func(in *deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error) {
		got = in
		return &deliveryv1.UpdateEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, ApplicationId: 7, Url: in.GetUrl(), Revision: 2, EventTypes: in.GetEventTypes().GetValues(), CreatedAt: now}}, nil
	}
	url := "https://new.example.com/hooks"
	types := []string{"a"}

	e, err := svc.UpdateEndpoint(context.Background(), actor, services.UpdateEndpointInput{EndpointID: 3, URL: &url, EventTypes: &types})
	require.NoError(t, err)
	require.Equal(t, int32(2), e.Revision)
	require.Equal(t, []string{"a"}, e.EventTypes)
	require.Equal(t, url, got.GetUrl())
	require.Nil(t, got.Description)
	require.Equal(t, []string{"a"}, got.GetEventTypes().GetValues())

	_, err = svc.UpdateEndpoint(context.Background(), actor, services.UpdateEndpointInput{EndpointID: 3, URL: &url})
	require.NoError(t, err)
	require.Nil(t, got.EventTypes, "absent list is not sent")

	_, err = svc.UpdateEndpoint(context.Background(), services.Actor{UserID: "u_ada"}, services.UpdateEndpointInput{EndpointID: 3})
	require.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestDeliveryRotateEndpointSecret(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}
	client.rotate = func(in *deliveryv1.RotateEndpointSecretRequest) (*deliveryv1.RotateEndpointSecretResponse, error) {
		require.Equal(t, 24*time.Hour, in.GetRetireIn().AsDuration())
		return &deliveryv1.RotateEndpointSecretResponse{Endpoint: &deliveryv1.Endpoint{Id: in.GetEndpointId(), ApplicationId: 7, Url: "https://r", Revision: 1, CreatedAt: now}, SigningSecret: "whsec_new", SecretVersion: 2, RetireAt: now}, nil
	}

	got, err := svc.RotateEndpointSecret(context.Background(), actor, 3, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, "whsec_new", got.SigningSecret)
	require.Equal(t, int32(2), got.SecretVersion)
	require.Equal(t, int64(3), got.Endpoint.ID)
	require.WithinDuration(t, now.AsTime(), got.RetireAt, time.Second)

	_, err = svc.RotateEndpointSecret(context.Background(), services.Actor{UserID: "u_ada"}, 3, 0)
	require.ErrorIs(t, err, apperr.ErrForbidden)
}

func TestDeliveryEnvironments(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	actor := services.Actor{UserID: "u_ada", WorkspaceID: ws}
	client.createEnv = func(in *deliveryv1.CreateEnvironmentRequest) (*deliveryv1.CreateEnvironmentResponse, error) {
		return &deliveryv1.CreateEnvironmentResponse{Environment: &deliveryv1.Environment{Id: 2, WorkspaceId: ws, Name: in.GetName(), Kind: in.GetKind(), CreatedAt: now}}, nil
	}
	client.listEnvs = func(*deliveryv1.ListEnvironmentsRequest) (*deliveryv1.ListEnvironmentsResponse, error) {
		return &deliveryv1.ListEnvironmentsResponse{Environments: []*deliveryv1.Environment{{Id: 1, Kind: "development", CreatedAt: now}, {Id: 2, Kind: "production", CreatedAt: now}}}, nil
	}
	var got *deliveryv1.CreateApplicationRequest
	client.create = func(in *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
		got = in
		return &deliveryv1.CreateApplicationResponse{Application: &deliveryv1.Application{Id: 1, WorkspaceId: ws, Name: in.GetName(), EnvironmentId: in.GetEnvironmentId(), EnvironmentKind: "production", CreatedAt: now}}, nil
	}

	e, err := svc.CreateEnvironment(context.Background(), actor, "production", "production")
	require.NoError(t, err)
	require.Equal(t, "production", e.Kind)
	list, err := svc.ListEnvironments(context.Background(), actor)
	require.NoError(t, err)
	require.Len(t, list, 2)

	app, err := svc.CreateApplicationIn(context.Background(), actor, 2, "Customer A")
	require.NoError(t, err)
	require.Equal(t, int64(2), *got.EnvironmentId)
	require.Equal(t, "production", app.EnvironmentKind)
	_, err = svc.CreateApplication(context.Background(), actor, "Customer B")
	require.NoError(t, err)
	require.Nil(t, got.EnvironmentId, "the plain route leaves the environment to the data plane's default")
}

func TestDeliveryListMessages(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	var got *deliveryv1.ListMessagesRequest
	client.list = func(in *deliveryv1.ListMessagesRequest) (*deliveryv1.ListMessagesResponse, error) {
		got = in
		return &deliveryv1.ListMessagesResponse{Total: 1, Messages: []*deliveryv1.MessageSummary{{
			Message: &deliveryv1.Message{Id: 1, PublicId: "msg_x", ApplicationId: 7, EventType: "order.paid", AcceptedAt: now, DeliveryCount: 3, EnvironmentId: 2, EnvironmentKind: "production"},
			Counts:  &deliveryv1.DeliveryCounts{Succeeded: 2, Failed: 1}, PayloadExpiresAt: now,
		}}}, nil
	}
	after := now.AsTime()
	items, total, err := svc.ListMessages(context.Background(), ws, services.ListMessagesInput{ApplicationID: 7, Type: "order.paid", AcceptedAfter: &after, Outcome: "failed", Limit: 20, SortBy: "acceptedAt", SortDir: "desc"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, int32(2), items[0].Counts.Succeeded)
	require.Equal(t, "production", items[0].EnvironmentKind)
	require.Equal(t, "failed", got.GetOutcome())
	require.NotNil(t, got.AcceptedAfter)
	require.Nil(t, got.AcceptedBefore)
	require.Equal(t, []string{ws}, client.md.Get("x-hookbeam-workspace-id"))

	_, _, err = svc.ListMessages(context.Background(), "", services.ListMessagesInput{ApplicationID: 7})
	require.ErrorIs(t, err, apperr.ErrUnauthorized)
}

func TestDeliveryListAuditEvents(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	id := int64(3)
	client.audit = func(in *deliveryv1.ListAuditEventsRequest) (*deliveryv1.ListAuditEventsResponse, error) {
		require.Equal(t, "endpoint", in.GetResourceType())
		require.NotNil(t, in.After)
		return &deliveryv1.ListAuditEventsResponse{Total: 1, Events: []*deliveryv1.AuditEvent{{Id: 1, ActorId: "u", Action: "endpoint.pause", ResourceType: "endpoint", ResourceId: &id, Details: []byte(`{"a":1}`), OccurredAt: now}}}, nil
	}
	after := now.AsTime()
	items, total, err := svc.ListAuditEvents(context.Background(), ws, services.ListAuditInput{ResourceType: "endpoint", After: &after, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, int64(3), *items[0].ResourceID)
	require.JSONEq(t, `{"a":1}`, string(items[0].Details))
}

func TestDeliveryUpdateApplication(t *testing.T) {
	svc, client := newDelivery(t)
	on := true
	client.updateApp = func(in *deliveryv1.UpdateApplicationRequest) (*deliveryv1.UpdateApplicationResponse, error) {
		require.Equal(t, int64(7), in.GetApplicationId())
		require.True(t, in.GetResponsePreviewsEnabled())
		return &deliveryv1.UpdateApplicationResponse{Application: &deliveryv1.Application{Id: 7, ResponsePreviewsEnabled: true, CreatedAt: timestamppb.Now()}}, nil
	}
	a, err := svc.UpdateApplication(context.Background(), developer, 7, services.UpdateApplicationInput{ResponsePreviewsEnabled: &on})
	require.NoError(t, err)
	require.True(t, a.ResponsePreviewsEnabled)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID))
}

func TestDeliveryGetMessagePayload(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	client.payload = func(in *deliveryv1.GetMessagePayloadRequest) (*deliveryv1.GetMessagePayloadResponse, error) {
		if in.GetMessageId() != 42 {
			return nil, status.Error(codes.FailedPrecondition, "payload has expired")
		}
		return &deliveryv1.GetMessagePayloadResponse{Message: &deliveryv1.Message{Id: 42, PublicId: "msg_x", AcceptedAt: now}, Payload: []byte(`{"a": 1}`), PayloadExpiresAt: now}, nil
	}
	p, err := svc.GetMessagePayload(context.Background(), developer, 42)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"a": 1}`), p.Body, "byte for byte")
	require.Equal(t, "msg_x", p.Message.WebhookID)

	_, err = svc.GetMessagePayload(context.Background(), developer, 43)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 410, appErr.Code)
	require.Equal(t, "payload has expired", appErr.Message)

	_, err = svc.GetMessagePayload(context.Background(), services.Actor{UserID: viewer, WorkspaceID: ws}, 42)
	require.ErrorIs(t, err, apperr.ErrForbidden, "a viewer is refused before the data plane")

	keyActor := services.Actor{WorkspaceID: ws}
	_, err = svc.GetMessagePayload(context.Background(), keyActor, 42)
	require.NoError(t, err, "a key passed the scope gate already")
}

func TestDeliveryListAttempts(t *testing.T) {
	preview := "later"
	tests := []struct {
		name         string
		actor        services.Actor
		scopes       []string
		wantPreviews bool
	}{
		{name: "a developer asks for previews", actor: developer, wantPreviews: true},
		{name: "a viewer does not", actor: services.Actor{UserID: viewer, WorkspaceID: ws}},
		{name: "a key with payloads:read asks for previews", actor: services.Actor{WorkspaceID: ws}, scopes: []string{"messages:read", "payloads:read"}, wantPreviews: true},
		{name: "a key without it does not", actor: services.Actor{WorkspaceID: ws}, scopes: []string{"messages:read"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, client := newDelivery(t)
			client.attempts = func(in *deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error) {
				require.Equal(t, int64(5), in.GetDeliveryId())
				require.Equal(t, tt.wantPreviews, in.GetIncludePreviews())
				a := &deliveryv1.Attempt{Id: 1, DeliveryId: 5, StartedAt: timestamppb.Now()}
				if in.GetIncludePreviews() {
					a.ResponsePreview = &preview
				}
				return &deliveryv1.ListAttemptsResponse{Attempts: []*deliveryv1.Attempt{a}}, nil
			}
			got, err := svc.ListAttempts(context.Background(), tt.actor, services.ListAttemptsInput{DeliveryID: 5, KeyScopes: tt.scopes})
			require.NoError(t, err)
			require.Len(t, got, 1)
			if tt.wantPreviews {
				require.Equal(t, "later", *got[0].ResponsePreview)
			} else {
				require.Nil(t, got[0].ResponsePreview)
			}
		})
	}
}

func TestDeliveryGetEndpoint(t *testing.T) {
	svc, client := newDelivery(t)
	now := timestamppb.Now()
	client.getEp = func(in *deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error) {
		require.Equal(t, int64(3), in.GetEndpointId())
		return &deliveryv1.GetEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, Url: "https://r", CreatedAt: now, Health: &deliveryv1.EndpointHealth{State: "unhealthy", ObservedAt: now, Stale: true, ConsecutiveFailures: 4}}}, nil
	}
	e, err := svc.GetEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Equal(t, "unhealthy", e.Health.State)
	require.True(t, e.Health.Stale)
	require.Equal(t, int32(4), e.Health.ConsecutiveFailures)
	require.NotNil(t, e.Health.ObservedAt)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	client.getEp = func(*deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error) {
		return &deliveryv1.GetEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, CreatedAt: now}}, nil
	}
	e, err = svc.GetEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Equal(t, "unknown", e.Health.State, "an endpoint without health reads as unknown")
}
