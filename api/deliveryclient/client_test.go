package deliveryclient_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/deliveryclient"
	"github.com/bete7512/hookbeam/api/services"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

const (
	ws    = "ws_1"
	owner = "u_owner"
)

// developer is a user's scope; system is a key's or a worker's scope, with no acting user.
var (
	developer = services.Scope{WorkspaceID: ws, ActorID: "u_ada"}
	system    = services.Scope{WorkspaceID: ws}
)

// fakeClient answers the contract from function fields and records the outgoing metadata.
type fakeClient struct {
	deliveryv1.DeliveryServiceClient
	md                 metadata.MD
	accept             func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error)
	create             func(*deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error)
	updateApp          func(*deliveryv1.UpdateApplicationRequest) (*deliveryv1.UpdateApplicationResponse, error)
	getEp              func(*deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error)
	episode            func(*deliveryv1.GetHealthEpisodeRequest) (*deliveryv1.GetHealthEpisodeResponse, error)
	limits             func(*deliveryv1.GetWorkspaceLimitsRequest) (*deliveryv1.GetWorkspaceLimitsResponse, error)
	archiveApplication func(*deliveryv1.ArchiveApplicationRequest) (*deliveryv1.ArchiveApplicationResponse, error)
	getApp             func(*deliveryv1.GetApplicationRequest) (*deliveryv1.GetApplicationResponse, error)
	listApps           func(*deliveryv1.ListApplicationsRequest) (*deliveryv1.ListApplicationsResponse, error)
	listEps            func(*deliveryv1.ListEndpointsRequest) (*deliveryv1.ListEndpointsResponse, error)
	overview           func(*deliveryv1.GetOverviewRequest) (*deliveryv1.GetOverviewResponse, error)
	sendTest           func(*deliveryv1.SendTestMessageRequest) (*deliveryv1.SendTestMessageResponse, error)
	payload            func(*deliveryv1.GetMessagePayloadRequest) (*deliveryv1.GetMessagePayloadResponse, error)
	attempts           func(*deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error)
	createEnv          func(*deliveryv1.CreateEnvironmentRequest) (*deliveryv1.CreateEnvironmentResponse, error)
	listEnvs           func(*deliveryv1.ListEnvironmentsRequest) (*deliveryv1.ListEnvironmentsResponse, error)
	get                func(*deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error)
	list               func(*deliveryv1.ListMessagesRequest) (*deliveryv1.ListMessagesResponse, error)
	audit              func(*deliveryv1.ListAuditEventsRequest) (*deliveryv1.ListAuditEventsResponse, error)
	cancel             func(*deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error)
	replay             func(*deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error)
	update             func(*deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error)
	rotate             func(*deliveryv1.RotateEndpointSecretRequest) (*deliveryv1.RotateEndpointSecretResponse, error)
	resume             func(*deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error)
	archive            func(*deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error)
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

func (f *fakeClient) GetApplication(ctx context.Context, in *deliveryv1.GetApplicationRequest, _ ...grpc.CallOption) (*deliveryv1.GetApplicationResponse, error) {
	f.record(ctx)
	return f.getApp(in)
}

func (f *fakeClient) ListApplications(ctx context.Context, in *deliveryv1.ListApplicationsRequest, _ ...grpc.CallOption) (*deliveryv1.ListApplicationsResponse, error) {
	f.record(ctx)
	return f.listApps(in)
}

func (f *fakeClient) ListEndpoints(ctx context.Context, in *deliveryv1.ListEndpointsRequest, _ ...grpc.CallOption) (*deliveryv1.ListEndpointsResponse, error) {
	f.record(ctx)
	return f.listEps(in)
}

func (f *fakeClient) GetOverview(ctx context.Context, in *deliveryv1.GetOverviewRequest, _ ...grpc.CallOption) (*deliveryv1.GetOverviewResponse, error) {
	f.record(ctx)
	return f.overview(in)
}

func (f *fakeClient) SendTestMessage(ctx context.Context, in *deliveryv1.SendTestMessageRequest, _ ...grpc.CallOption) (*deliveryv1.SendTestMessageResponse, error) {
	f.record(ctx)
	return f.sendTest(in)
}

func (f *fakeClient) GetWorkspaceLimits(ctx context.Context, in *deliveryv1.GetWorkspaceLimitsRequest, _ ...grpc.CallOption) (*deliveryv1.GetWorkspaceLimitsResponse, error) {
	f.record(ctx)
	return f.limits(in)
}

func (f *fakeClient) GetHealthEpisode(ctx context.Context, in *deliveryv1.GetHealthEpisodeRequest, _ ...grpc.CallOption) (*deliveryv1.GetHealthEpisodeResponse, error) {
	f.record(ctx)
	return f.episode(in)
}

func (f *fakeClient) GetEndpoint(ctx context.Context, in *deliveryv1.GetEndpointRequest, _ ...grpc.CallOption) (*deliveryv1.GetEndpointResponse, error) {
	f.record(ctx)
	return f.getEp(in)
}

func (f *fakeClient) ArchiveApplication(ctx context.Context, in *deliveryv1.ArchiveApplicationRequest, _ ...grpc.CallOption) (*deliveryv1.ArchiveApplicationResponse, error) {
	f.record(ctx)
	if f.archiveApplication == nil {
		return nil, status.Error(codes.Unimplemented, "archive application")
	}
	return f.archiveApplication(in)
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

func newClient(t *testing.T) (services.DataPlane, *fakeClient) {
	t.Helper()
	client := &fakeClient{}
	dp, err := deliveryclient.New(client, time.Second)
	require.NoError(t, err)
	return dp, client
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		client  deliveryv1.DeliveryServiceClient
		timeout time.Duration
		wantErr bool
	}{
		{name: "client and timeout", client: &fakeClient{}, timeout: time.Second},
		{name: "nil client", timeout: time.Second, wantErr: true},
		{name: "zero timeout", client: &fakeClient{}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := deliveryclient.New(tt.client, tt.timeout)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCreateApplication(t *testing.T) {
	dp, client := newClient(t)
	now := time.Now().UTC().Truncate(time.Second)
	var got *deliveryv1.CreateApplicationRequest
	client.create = func(in *deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
		got = in
		return &deliveryv1.CreateApplicationResponse{Application: &deliveryv1.Application{Id: 1, WorkspaceId: ws, Name: in.GetName(), EnvironmentId: in.GetEnvironmentId(), EnvironmentKind: "production", CreatedBy: owner, CreatedAt: timestamppb.New(now)}}, nil
	}
	app, err := dp.CreateApplication(context.Background(), services.Scope{WorkspaceID: ws, ActorID: owner}, nil, "Customer A")
	require.NoError(t, err)
	require.Equal(t, int64(1), app.ID)
	require.Equal(t, now, app.CreatedAt)
	require.Nil(t, app.ArchivedAt)
	require.Nil(t, got.EnvironmentId, "no environment leaves the data plane's default")
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID), "workspace travels as metadata")
	require.Equal(t, []string{owner}, client.md.Get(grpcx.MetadataActorID))

	env := int64(2)
	app, err = dp.CreateApplication(context.Background(), developer, &env, "Customer A")
	require.NoError(t, err)
	require.Equal(t, int64(2), *got.EnvironmentId)
	require.Equal(t, "production", app.EnvironmentKind)

	client.create = func(*deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error) {
		return nil, status.Error(codes.AlreadyExists, "exists")
	}
	_, err = dp.CreateApplication(context.Background(), developer, nil, "Customer A")
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.Code)
}

func TestAcceptMessage(t *testing.T) {
	dp, client := newClient(t)
	payload := []byte(`{"type":"order.shipped","external_id":"order-817","data":{"n":1}}`)
	external := "order-817"
	client.accept = func(in *deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
		require.Equal(t, int64(7), in.GetApplicationId())
		require.Equal(t, "order.shipped", in.GetEventType())
		require.Equal(t, "order-817", in.GetSenderEventId())
		require.Equal(t, "k1", in.GetIdempotencyKey())
		require.Equal(t, payload, in.GetPayload(), "bytes are forwarded untouched")
		return &deliveryv1.AcceptMessageResponse{Message: &deliveryv1.Message{Id: 42, PublicId: "msg_x", ApplicationId: 7, EventType: "order.shipped", DeliveryCount: 2, AcceptedAt: timestamppb.Now()}, Duplicate: true}, nil
	}
	in := services.AcceptMessageInput{ApplicationID: 7, EventType: "order.shipped", IdempotencyKey: "k1", SenderEventID: &external, Payload: payload}
	res, err := dp.AcceptMessage(context.Background(), system, in)
	require.NoError(t, err)
	require.Equal(t, int64(42), res.Message.ID)
	require.Equal(t, "msg_x", res.Message.WebhookID)
	require.True(t, res.Duplicate)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID))
	require.Empty(t, client.md.Get(grpcx.MetadataActorID), "a key has no acting user")

	var appErr *apperr.AppError
	client.accept = func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
		return nil, grpcx.Status(apperr.NewTooManyRequests(30 * time.Second))
	}
	_, err = dp.AcceptMessage(context.Background(), system, in)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 429, appErr.Code)
	require.Equal(t, 30*time.Second, appErr.RetryAfter)

	client.accept = func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error) {
		return nil, status.Error(codes.Unavailable, "down")
	}
	_, err = dp.AcceptMessage(context.Background(), system, in)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 503, appErr.Code)
	require.Equal(t, grpcx.UnavailableRetryAfter, appErr.RetryAfter, "a down data plane tells the sender when to retry instead of buffering")
}

func TestGetMessage(t *testing.T) {
	dp, client := newClient(t)
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
	m, ds, err := dp.GetMessage(context.Background(), system, 42)
	require.NoError(t, err)
	require.Equal(t, "msg_x", m.WebhookID)
	require.Len(t, ds, 1)
	require.Equal(t, "retry_scheduled", ds[0].State)
	require.Equal(t, "http_503", *ds[0].FailureReason)
	require.NotNil(t, ds[0].DueAt)
	require.Nil(t, ds[0].CompletedAt)

	_, _, err = dp.GetMessage(context.Background(), system, -1)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.Code)
	require.Equal(t, "message not found", appErr.Message)
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestCancelDelivery(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	reason := "cancelled"
	client.cancel = func(in *deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error) {
		if in.GetDeliveryId() != 5 {
			return nil, status.Error(codes.NotFound, "delivery not found")
		}
		return &deliveryv1.CancelDeliveryResponse{Delivery: &deliveryv1.Delivery{Id: 5, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED, AttemptCount: 1, DeadlineAt: now, CreatedAt: now, CompletedAt: now, FailureReason: &reason}}, nil
	}

	d, err := dp.CancelDelivery(context.Background(), developer, 5)
	require.NoError(t, err)
	require.Equal(t, "cancelled", d.State)
	require.Equal(t, "cancelled", *d.FailureReason)
	require.NotNil(t, d.CompletedAt)
	require.Equal(t, []string{ws}, client.md.Get("x-hookbeam-workspace-id"))
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	_, err = dp.CancelDelivery(context.Background(), developer, -1)
	require.ErrorIs(t, err, apperr.ErrNotFound)
}

func TestReplayDelivery(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	original := int64(5)
	client.replay = func(in *deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error) {
		if in.GetIdempotencyKey() != "r1" {
			return nil, status.Error(codes.AlreadyExists, "an active delivery cycle already exists for this endpoint")
		}
		return &deliveryv1.ReplayDeliveryResponse{Delivery: &deliveryv1.Delivery{Id: 9, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED, DueAt: now, DeadlineAt: now, CreatedAt: now, ReplayOfDeliveryId: &original}}, nil
	}

	d, err := dp.ReplayDelivery(context.Background(), developer, 5, "r1")
	require.NoError(t, err)
	require.Equal(t, int64(9), d.ID)
	require.Equal(t, int64(5), *d.ReplayOfDeliveryID)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	_, err = dp.ReplayDelivery(context.Background(), developer, 5, "r2")
	require.ErrorIs(t, err, apperr.ErrConflict)
}

func TestResumeAndArchiveEndpoint(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.resume = func(in *deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error) {
		if in.GetEndpointId() != 3 {
			return nil, status.Error(codes.AlreadyExists, "endpoint is archived")
		}
		return &deliveryv1.ResumeEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, ApplicationId: 7, Url: "https://r", Revision: 1, CreatedAt: now}}, nil
	}
	client.archive = func(in *deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error) {
		return &deliveryv1.ArchiveEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: in.GetEndpointId(), ApplicationId: 7, Url: "https://r", Revision: 1, ArchivedAt: now, CreatedAt: now}, CancelledDeliveries: 2}, nil
	}

	e, err := dp.ResumeEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Nil(t, e.PausedAt)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))
	_, err = dp.ResumeEndpoint(context.Background(), developer, 4)
	require.ErrorIs(t, err, apperr.ErrConflict)

	e, cancelled, err := dp.ArchiveEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.NotNil(t, e.ArchivedAt)
	require.Equal(t, int64(2), cancelled)
}

func TestUpdateEndpoint(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	var got *deliveryv1.UpdateEndpointRequest
	client.update = func(in *deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error) {
		got = in
		return &deliveryv1.UpdateEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, ApplicationId: 7, Url: in.GetUrl(), Revision: 2, EventTypes: in.GetEventTypes().GetValues(), CreatedAt: now}}, nil
	}
	url := "https://new.example.com/hooks"
	types := []string{"a"}

	e, err := dp.UpdateEndpoint(context.Background(), developer, services.UpdateEndpointInput{EndpointID: 3, URL: &url, EventTypes: &types})
	require.NoError(t, err)
	require.Equal(t, int32(2), e.Revision)
	require.Equal(t, []string{"a"}, e.EventTypes)
	require.Equal(t, url, got.GetUrl())
	require.Nil(t, got.Description)
	require.Equal(t, []string{"a"}, got.GetEventTypes().GetValues())

	_, err = dp.UpdateEndpoint(context.Background(), developer, services.UpdateEndpointInput{EndpointID: 3, URL: &url})
	require.NoError(t, err)
	require.Nil(t, got.EventTypes, "absent list is not sent")

	expected := int32(2)
	client.update = func(in *deliveryv1.UpdateEndpointRequest) (*deliveryv1.UpdateEndpointResponse, error) {
		require.Equal(t, int32(2), in.GetExpectedRevision())
		return nil, status.Error(codes.AlreadyExists, "endpoint changed since it was loaded")
	}
	_, err = dp.UpdateEndpoint(context.Background(), developer, services.UpdateEndpointInput{EndpointID: 3, ExpectedRevision: &expected})
	require.ErrorIs(t, err, apperr.ErrConflict)
}

func TestRotateEndpointSecret(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.rotate = func(in *deliveryv1.RotateEndpointSecretRequest) (*deliveryv1.RotateEndpointSecretResponse, error) {
		require.Equal(t, 24*time.Hour, in.GetRetireIn().AsDuration())
		return &deliveryv1.RotateEndpointSecretResponse{Endpoint: &deliveryv1.Endpoint{Id: in.GetEndpointId(), ApplicationId: 7, Url: "https://r", Revision: 1, CreatedAt: now}, SigningSecret: "whsec_new", SecretVersion: 2, RetireAt: now}, nil
	}

	got, err := dp.RotateEndpointSecret(context.Background(), developer, 3, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, "whsec_new", got.SigningSecret)
	require.Equal(t, int32(2), got.SecretVersion)
	require.Equal(t, int64(3), got.Endpoint.ID)
	require.WithinDuration(t, now.AsTime(), got.RetireAt, time.Second)
}

func TestEnvironments(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.createEnv = func(in *deliveryv1.CreateEnvironmentRequest) (*deliveryv1.CreateEnvironmentResponse, error) {
		return &deliveryv1.CreateEnvironmentResponse{Environment: &deliveryv1.Environment{Id: 2, WorkspaceId: ws, Name: in.GetName(), Kind: in.GetKind(), CreatedAt: now}}, nil
	}
	client.listEnvs = func(*deliveryv1.ListEnvironmentsRequest) (*deliveryv1.ListEnvironmentsResponse, error) {
		return &deliveryv1.ListEnvironmentsResponse{Environments: []*deliveryv1.Environment{{Id: 1, Kind: "development", CreatedAt: now}, {Id: 2, Kind: "production", CreatedAt: now}}}, nil
	}

	e, err := dp.CreateEnvironment(context.Background(), developer, "production", "production")
	require.NoError(t, err)
	require.Equal(t, "production", e.Kind)
	list, err := dp.ListEnvironments(context.Background(), developer)
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestListMessages(t *testing.T) {
	dp, client := newClient(t)
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
	items, total, err := dp.ListMessages(context.Background(), system, services.ListMessagesInput{ApplicationID: 7, Type: "order.paid", AcceptedAfter: &after, Outcome: "failed", Limit: 20, SortBy: "acceptedAt", SortDir: "desc"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, int32(2), items[0].Counts.Succeeded)
	require.Equal(t, "production", items[0].EnvironmentKind)
	require.Equal(t, "failed", got.GetOutcome())
	require.Equal(t, int32(20), got.GetLimit())
	require.NotNil(t, got.AcceptedAfter)
	require.Nil(t, got.AcceptedBefore)
	require.Equal(t, []string{ws}, client.md.Get("x-hookbeam-workspace-id"))
}

func TestListAuditEvents(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	id := int64(3)
	client.audit = func(in *deliveryv1.ListAuditEventsRequest) (*deliveryv1.ListAuditEventsResponse, error) {
		require.Equal(t, "endpoint", in.GetResourceType())
		require.NotNil(t, in.After)
		return &deliveryv1.ListAuditEventsResponse{Total: 1, Events: []*deliveryv1.AuditEvent{{Id: 1, ActorId: "u", Action: "endpoint.pause", ResourceType: "endpoint", ResourceId: &id, Details: []byte(`{"a":1}`), OccurredAt: now}}}, nil
	}
	after := now.AsTime()
	items, total, err := dp.ListAuditEvents(context.Background(), system, services.ListAuditInput{ResourceType: "endpoint", After: &after, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, int64(3), *items[0].ResourceID)
	require.JSONEq(t, `{"a":1}`, string(items[0].Details))
}

func TestUpdateApplication(t *testing.T) {
	dp, client := newClient(t)
	on := true
	client.updateApp = func(in *deliveryv1.UpdateApplicationRequest) (*deliveryv1.UpdateApplicationResponse, error) {
		require.Equal(t, int64(7), in.GetApplicationId())
		require.True(t, in.GetResponsePreviewsEnabled())
		return &deliveryv1.UpdateApplicationResponse{Application: &deliveryv1.Application{Id: 7, ResponsePreviewsEnabled: true, CreatedAt: timestamppb.Now()}}, nil
	}
	a, err := dp.UpdateApplication(context.Background(), developer, 7, services.UpdateApplicationInput{ResponsePreviewsEnabled: &on})
	require.NoError(t, err)
	require.True(t, a.ResponsePreviewsEnabled)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID))
}

func TestGetMessagePayload(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.payload = func(in *deliveryv1.GetMessagePayloadRequest) (*deliveryv1.GetMessagePayloadResponse, error) {
		if in.GetMessageId() != 42 {
			return nil, status.Error(codes.FailedPrecondition, "payload has expired")
		}
		return &deliveryv1.GetMessagePayloadResponse{Message: &deliveryv1.Message{Id: 42, PublicId: "msg_x", AcceptedAt: now}, Payload: []byte(`{"a": 1}`), PayloadExpiresAt: now}, nil
	}
	p, err := dp.GetMessagePayload(context.Background(), developer, 42)
	require.NoError(t, err)
	require.Equal(t, []byte(`{"a": 1}`), p.Body, "byte for byte")
	require.Equal(t, "msg_x", p.Message.WebhookID)

	_, err = dp.GetMessagePayload(context.Background(), developer, 43)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 410, appErr.Code)
	require.Equal(t, "payload has expired", appErr.Message)
}

func TestListAttempts(t *testing.T) {
	preview := "later"
	tests := []struct {
		name     string
		previews bool
	}{
		{name: "previews requested", previews: true},
		{name: "previews withheld"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dp, client := newClient(t)
			client.attempts = func(in *deliveryv1.ListAttemptsRequest) (*deliveryv1.ListAttemptsResponse, error) {
				require.Equal(t, int64(5), in.GetDeliveryId())
				require.Equal(t, tt.previews, in.GetIncludePreviews())
				a := &deliveryv1.Attempt{Id: 1, DeliveryId: 5, StartedAt: timestamppb.Now(), ResponseHeaders: map[string]string{"content-type": "text/plain"}}
				if in.GetIncludePreviews() {
					a.ResponsePreview = &preview
				}
				return &deliveryv1.ListAttemptsResponse{Attempts: []*deliveryv1.Attempt{a}}, nil
			}
			got, err := dp.ListAttempts(context.Background(), developer, 5, tt.previews)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, "text/plain", got[0].ResponseHeaders["content-type"])
			if tt.previews {
				require.Equal(t, "later", *got[0].ResponsePreview)
			} else {
				require.Nil(t, got[0].ResponsePreview)
			}
		})
	}
}

func TestGetEndpoint(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.getEp = func(in *deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error) {
		require.Equal(t, int64(3), in.GetEndpointId())
		return &deliveryv1.GetEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, Url: "https://r", CreatedAt: now, Health: &deliveryv1.EndpointHealth{State: "unhealthy", ObservedAt: now, Stale: true, ConsecutiveFailures: 4}}}, nil
	}
	e, err := dp.GetEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Equal(t, "unhealthy", e.Health.State)
	require.True(t, e.Health.Stale)
	require.Equal(t, int32(4), e.Health.ConsecutiveFailures)
	require.NotNil(t, e.Health.ObservedAt)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))

	client.getEp = func(*deliveryv1.GetEndpointRequest) (*deliveryv1.GetEndpointResponse, error) {
		return &deliveryv1.GetEndpointResponse{Endpoint: &deliveryv1.Endpoint{Id: 3, CreatedAt: now}}, nil
	}
	e, err = dp.GetEndpoint(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Equal(t, "unknown", e.Health.State, "an endpoint without health reads as unknown")
	require.Equal(t, []string{}, e.EventTypes, "no subscriptions is an empty list, not null")
}

func TestGetHealthEpisode(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.episode = func(in *deliveryv1.GetHealthEpisodeRequest) (*deliveryv1.GetHealthEpisodeResponse, error) {
		require.Equal(t, int64(9), in.GetEpisodeId())
		return &deliveryv1.GetHealthEpisodeResponse{
			Episode:  &deliveryv1.HealthEpisode{Id: 9, EndpointId: 3, Revision: 2, ConsecutiveFailures: 4, OpenedAt: now, ResolvedAt: now},
			Endpoint: &deliveryv1.Endpoint{Id: 3, ApplicationId: 7, Url: "https://r", CreatedAt: now},
		}, nil
	}
	episode, endpoint, err := dp.GetHealthEpisode(context.Background(), system, 9)
	require.NoError(t, err)
	require.Equal(t, int64(9), episode.ID)
	require.Equal(t, int32(4), episode.ConsecutiveFailures)
	require.NotNil(t, episode.ResolvedAt)
	require.Equal(t, "https://r", endpoint.URL)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID))
	require.Empty(t, client.md.Get("x-hookbeam-actor-id"), "a system read carries no actor")
}

func TestArchiveApplication(t *testing.T) {
	dp, client := newClient(t)
	client.archiveApplication = func(in *deliveryv1.ArchiveApplicationRequest) (*deliveryv1.ArchiveApplicationResponse, error) {
		return &deliveryv1.ArchiveApplicationResponse{Application: &deliveryv1.Application{Id: in.GetApplicationId(), ArchivedAt: timestamppb.Now()}, ArchivedEndpoints: 2, CancelledDeliveries: 5}, nil
	}
	a, endpoints, cancelled, err := dp.ArchiveApplication(context.Background(), developer, 7)
	require.NoError(t, err)
	require.NotNil(t, a.ArchivedAt)
	require.Equal(t, int64(2), endpoints)
	require.Equal(t, int64(5), cancelled)
	require.Equal(t, []string{ws}, client.md.Get(grpcx.MetadataWorkspaceID))
}

func TestGetWorkspaceLimits(t *testing.T) {
	dp, client := newClient(t)
	client.limits = func(*deliveryv1.GetWorkspaceLimitsRequest) (*deliveryv1.GetWorkspaceLimitsResponse, error) {
		return &deliveryv1.GetWorkspaceLimitsResponse{Limits: &deliveryv1.WorkspaceLimits{EndpointsPerApplication: 20, FanoutPerMessage: 20, BacklogPerWorkspace: 10000}, Backlog: 12, Applications: []*deliveryv1.ApplicationEndpointCount{{ApplicationId: 7, Name: "Customer A", Endpoints: 3}}, Retention: &deliveryv1.Retention{Payload: durationpb.New(168 * time.Hour), IdempotencyWindow: durationpb.New(168 * time.Hour), Audit: durationpb.New(2160 * time.Hour)}}, nil
	}
	l, err := dp.GetWorkspaceLimits(context.Background(), developer)
	require.NoError(t, err)
	require.Equal(t, 10000, l.BacklogPerWorkspace)
	require.Equal(t, 168*time.Hour, l.Retention.Payload)
	require.Equal(t, 12, l.Backlog)
	require.Len(t, l.Applications, 1)
	require.Equal(t, "Customer A", l.Applications[0].Name)
}

func TestListsOverviewAndTest(t *testing.T) {
	dp, client := newClient(t)
	now := timestamppb.Now()
	client.getApp = func(in *deliveryv1.GetApplicationRequest) (*deliveryv1.GetApplicationResponse, error) {
		return &deliveryv1.GetApplicationResponse{Application: &deliveryv1.Application{Id: in.GetApplicationId(), Name: "A", CreatedAt: now}}, nil
	}
	a, err := dp.GetApplication(context.Background(), developer, 7)
	require.NoError(t, err)
	require.Equal(t, "A", a.Name)

	client.listApps = func(in *deliveryv1.ListApplicationsRequest) (*deliveryv1.ListApplicationsResponse, error) {
		require.Equal(t, int64(2), in.GetEnvironmentId())
		return &deliveryv1.ListApplicationsResponse{Applications: []*deliveryv1.Application{{Id: 7, CreatedAt: now}}}, nil
	}
	apps, err := dp.ListApplications(context.Background(), developer, 2)
	require.NoError(t, err)
	require.Len(t, apps, 1)

	waiting := int32(3)
	client.listEps = func(*deliveryv1.ListEndpointsRequest) (*deliveryv1.ListEndpointsResponse, error) {
		return &deliveryv1.ListEndpointsResponse{Endpoints: []*deliveryv1.Endpoint{{Id: 3, CreatedAt: now, Waiting: &waiting, Secrets: []*deliveryv1.EndpointSecretInfo{{Version: 1, Fingerprint: "f", CreatedAt: now}}}}}, nil
	}
	eps, err := dp.ListEndpoints(context.Background(), developer, 7)
	require.NoError(t, err)
	require.Equal(t, int32(3), *eps[0].Waiting)
	require.Equal(t, "f", eps[0].Secrets[0].Fingerprint)

	client.overview = func(*deliveryv1.GetOverviewRequest) (*deliveryv1.GetOverviewResponse, error) {
		return &deliveryv1.GetOverviewResponse{Overview: &deliveryv1.Overview{Since: now, ComputedAt: now, Accepted: 5, Backlog: 1, EndpointsUnhealthy: 1, HealthObservedAt: now, Series: []*deliveryv1.OverviewBucket{{Hour: now, Accepted: 5}}}}, nil
	}
	o, err := dp.GetOverview(context.Background(), developer, 2)
	require.NoError(t, err)
	require.Equal(t, int64(5), o.Accepted)
	require.NotNil(t, o.HealthObservedAt)
	require.Nil(t, o.OldestPendingDueAt)
	require.Len(t, o.Series, 1)

	client.sendTest = func(in *deliveryv1.SendTestMessageRequest) (*deliveryv1.SendTestMessageResponse, error) {
		require.Equal(t, int64(3), in.GetEndpointId())
		return &deliveryv1.SendTestMessageResponse{Message: &deliveryv1.Message{Id: 42, PublicId: "msg_t", AcceptedAt: now, DeliveryCount: 1}, Delivery: &deliveryv1.Delivery{Id: 9, MessageId: 42, EndpointId: 3, State: deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED, DeadlineAt: now, CreatedAt: now}}, nil
	}
	m, d, err := dp.SendTestMessage(context.Background(), developer, 3)
	require.NoError(t, err)
	require.Equal(t, "msg_t", m.WebhookID)
	require.Equal(t, "queued", d.State)
	require.Equal(t, []string{"u_ada"}, client.md.Get("x-hookbeam-actor-id"))
}
