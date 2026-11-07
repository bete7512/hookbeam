package services_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/services"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

// fakeClient answers the contract from function fields and records the outgoing metadata.
type fakeClient struct {
	deliveryv1.DeliveryServiceClient
	md      metadata.MD
	accept  func(*deliveryv1.AcceptMessageRequest) (*deliveryv1.AcceptMessageResponse, error)
	create  func(*deliveryv1.CreateApplicationRequest) (*deliveryv1.CreateApplicationResponse, error)
	get     func(*deliveryv1.GetMessageRequest) (*deliveryv1.GetMessageResponse, error)
	cancel  func(*deliveryv1.CancelDeliveryRequest) (*deliveryv1.CancelDeliveryResponse, error)
	replay  func(*deliveryv1.ReplayDeliveryRequest) (*deliveryv1.ReplayDeliveryResponse, error)
	resume  func(*deliveryv1.ResumeEndpointRequest) (*deliveryv1.ResumeEndpointResponse, error)
	archive func(*deliveryv1.ArchiveEndpointRequest) (*deliveryv1.ArchiveEndpointResponse, error)
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

func (f *fakeClient) CreateApplication(ctx context.Context, in *deliveryv1.CreateApplicationRequest, _ ...grpc.CallOption) (*deliveryv1.CreateApplicationResponse, error) {
	f.record(ctx)
	return f.create(in)
}

func (f *fakeClient) AcceptMessage(ctx context.Context, in *deliveryv1.AcceptMessageRequest, _ ...grpc.CallOption) (*deliveryv1.AcceptMessageResponse, error) {
	f.record(ctx)
	return f.accept(in)
}

func (f *fakeClient) GetMessage(ctx context.Context, in *deliveryv1.GetMessageRequest, _ ...grpc.CallOption) (*deliveryv1.GetMessageResponse, error) {
	f.record(ctx)
	return f.get(in)
}

func newDelivery(t *testing.T) (services.DeliveryService, *fakeClient) {
	t.Helper()
	client := &fakeClient{}
	svc, err := services.NewDeliveryService(services.DeliveryServiceDeps{Client: client, Timeout: time.Second, Logger: slog.New(slog.DiscardHandler)})
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
