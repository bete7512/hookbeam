package rpc_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/rpc"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/delivery/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

const ws = "ws_1"

var scope = services.Scope{WorkspaceID: ws, ActorID: "u_ada"}

type fixture struct {
	envs      *service_mocks.MockEnvironmentService
	apps      *service_mocks.MockApplicationService
	endpoints *service_mocks.MockEndpointService
	messages  *service_mocks.MockMessageService
	client    deliveryv1.DeliveryServiceClient
}

// newFixture serves the real server with the standard interceptors over an in-memory connection.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{envs: service_mocks.NewMockEnvironmentService(ctrl), apps: service_mocks.NewMockApplicationService(ctrl), endpoints: service_mocks.NewMockEndpointService(ctrl), messages: service_mocks.NewMockMessageService(ctrl)}
	srv, err := rpc.New(rpc.Deps{Environments: f.envs, Applications: f.apps, Endpoints: f.endpoints, Messages: f.messages, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	lis := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer(grpcx.ServerInterceptors(slog.New(slog.DiscardHandler)))
	deliveryv1.RegisterDeliveryServiceServer(grpcServer, srv)
	go func() { _ = grpcServer.Serve(lis) }()
	t.Cleanup(grpcServer.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	f.client = deliveryv1.NewDeliveryServiceClient(conn)
	return f
}

func scoped(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return grpcx.WithScope(ctx, grpcx.Scope{WorkspaceID: ws, ActorID: "u_ada"})
}

func TestNew(t *testing.T) {
	_, err := rpc.New(rpc.Deps{})
	require.Error(t, err)
}

func TestEnvironments(t *testing.T) {
	now := time.Now()
	f := newFixture(t)
	f.envs.EXPECT().CreateEnvironment(gomock.Any(), scope, "production", "production").Return(&models.Environment{ID: 2, WorkspaceID: ws, Name: "production", Kind: "production", CreatedBy: "u_ada", CreatedAt: now}, nil)
	created, err := f.client.CreateEnvironment(scoped(t), &deliveryv1.CreateEnvironmentRequest{Name: "production", Kind: "production"})
	require.NoError(t, err)
	require.Equal(t, "production", created.GetEnvironment().GetKind())

	f.envs.EXPECT().CreateEnvironment(gomock.Any(), scope, "x", "staging").Return(nil, apperr.NewValidation(map[string]string{"kind": "x"}))
	_, err = f.client.CreateEnvironment(scoped(t), &deliveryv1.CreateEnvironmentRequest{Name: "x", Kind: "staging"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	f.envs.EXPECT().ListEnvironments(gomock.Any(), scope).Return([]models.Environment{{ID: 1, Kind: "development", CreatedAt: now}, {ID: 2, Kind: "production", CreatedAt: now}}, nil)
	list, err := f.client.ListEnvironments(scoped(t), &deliveryv1.ListEnvironmentsRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetEnvironments(), 2)

	envID := int64(2)
	f.apps.EXPECT().CreateApplication(gomock.Any(), scope, int64(2), "Customer A").Return(&models.Application{ID: 1, WorkspaceID: ws, EnvironmentID: 2, EnvironmentKind: "production", Name: "Customer A", CreatedAt: now}, nil)
	app, err := f.client.CreateApplication(scoped(t), &deliveryv1.CreateApplicationRequest{Name: "Customer A", EnvironmentId: &envID})
	require.NoError(t, err)
	require.Equal(t, "production", app.GetApplication().GetEnvironmentKind())
}

func TestCreateApplication(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		setup    func(f *fixture)
		noScope  bool
		wantCode codes.Code
	}{
		{name: "created", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, int64(0), "Customer A").Return(&models.Application{ID: 1, WorkspaceID: ws, Name: "Customer A", CreatedBy: "u_ada", CreatedAt: now}, nil)
		}},
		{name: "validation", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, int64(0), "Customer A").Return(nil, apperr.NewValidation(map[string]string{"name": "x"}))
		}, wantCode: codes.InvalidArgument},
		{name: "conflict", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, int64(0), "Customer A").Return(nil, apperr.NewConflict("dup", nil))
		}, wantCode: codes.AlreadyExists},
		{name: "internal", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, int64(0), "Customer A").Return(nil, apperr.NewInternal(errors.New("boom")))
		}, wantCode: codes.Internal},
		{name: "no scope", noScope: true, wantCode: codes.Unauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			ctx := scoped(t)
			if tt.noScope {
				ctx = context.Background()
			}
			resp, err := f.client.CreateApplication(ctx, &deliveryv1.CreateApplicationRequest{Name: "Customer A"})
			require.Equal(t, tt.wantCode, status.Code(err), "%v", err)
			if tt.wantCode != codes.OK {
				return
			}
			require.Equal(t, int64(1), resp.GetApplication().GetId())
			require.Equal(t, ws, resp.GetApplication().GetWorkspaceId())
			require.Equal(t, now, resp.GetApplication().GetCreatedAt().AsTime())
			require.Nil(t, resp.GetApplication().ArchivedAt)
		})
	}
}

func TestCreateEndpoint(t *testing.T) {
	in := services.CreateEndpointInput{ApplicationID: 7, URL: "https://r.example.com/h", Description: "d", EventTypes: []string{"a"}}
	tests := []struct {
		name     string
		setup    func(f *fixture)
		wantCode codes.Code
	}{
		{name: "created", setup: func(f *fixture) {
			f.endpoints.EXPECT().CreateEndpoint(gomock.Any(), scope, in).Return(&models.Endpoint{ID: 3, ApplicationID: 7, URL: in.URL, Revision: 1, EventTypes: []string{"a"}, CreatedAt: time.Now()}, "whsec_x", int32(1), nil)
		}},
		{name: "unknown application", setup: func(f *fixture) {
			f.endpoints.EXPECT().CreateEndpoint(gomock.Any(), scope, in).Return(nil, "", int32(0), apperr.NewNotFound("application", nil))
		}, wantCode: codes.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			resp, err := f.client.CreateEndpoint(scoped(t), &deliveryv1.CreateEndpointRequest{ApplicationId: 7, Url: in.URL, Description: "d", EventTypes: []string{"a"}})
			require.Equal(t, tt.wantCode, status.Code(err), "%v", err)
			if tt.wantCode != codes.OK {
				return
			}
			require.Equal(t, "whsec_x", resp.GetSigningSecret())
			require.Equal(t, int32(1), resp.GetSecretVersion())
			require.Equal(t, int64(3), resp.GetEndpoint().GetId())
			require.Equal(t, []string{"a"}, resp.GetEndpoint().GetEventTypes())
			require.Nil(t, resp.GetEndpoint().PausedAt)
		})
	}
}

func TestPauseEndpoint(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		setup    func(f *fixture)
		wantCode codes.Code
	}{
		{name: "paused", setup: func(f *fixture) {
			f.endpoints.EXPECT().PauseEndpoint(gomock.Any(), scope, int64(3)).Return(&models.Endpoint{ID: 3, PausedAt: &now, CreatedAt: now}, nil)
		}},
		{name: "archived", setup: func(f *fixture) {
			f.endpoints.EXPECT().PauseEndpoint(gomock.Any(), scope, int64(3)).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, wantCode: codes.AlreadyExists},
		{name: "not found", setup: func(f *fixture) {
			f.endpoints.EXPECT().PauseEndpoint(gomock.Any(), scope, int64(3)).Return(nil, apperr.NewNotFound("endpoint", nil))
		}, wantCode: codes.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			resp, err := f.client.PauseEndpoint(scoped(t), &deliveryv1.PauseEndpointRequest{EndpointId: 3})
			require.Equal(t, tt.wantCode, status.Code(err), "%v", err)
			if tt.wantCode == codes.OK {
				require.NotNil(t, resp.GetEndpoint().PausedAt)
			}
		})
	}
}

func TestAcceptMessage(t *testing.T) {
	now := time.Now()
	payload := []byte(`{"type":"order.shipped","data":{}}`)
	in := services.AcceptInput{ApplicationID: 7, EventType: "order.shipped", IdempotencyKey: "k1", Payload: payload}
	tests := []struct {
		name     string
		setup    func(f *fixture)
		wantCode codes.Code
	}{
		{name: "accepted", setup: func(f *fixture) {
			f.messages.EXPECT().Accept(gomock.Any(), scope, in).Return(&services.AcceptResult{Message: &models.Message{ID: 42, PublicID: "msg_x", ApplicationID: 7, EventType: "order.shipped", AcceptedAt: now}, DeliveryCount: 2}, nil)
		}},
		{name: "no subscriber", setup: func(f *fixture) {
			f.messages.EXPECT().Accept(gomock.Any(), scope, in).Return(nil, apperr.NewValidation(map[string]string{"type": "no endpoint"}))
		}, wantCode: codes.InvalidArgument},
		{name: "key conflict", setup: func(f *fixture) {
			f.messages.EXPECT().Accept(gomock.Any(), scope, in).Return(nil, apperr.NewConflict("reused", nil))
		}, wantCode: codes.AlreadyExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			resp, err := f.client.AcceptMessage(scoped(t), &deliveryv1.AcceptMessageRequest{ApplicationId: 7, EventType: "order.shipped", IdempotencyKey: "k1", Payload: payload})
			require.Equal(t, tt.wantCode, status.Code(err), "%v", err)
			if tt.wantCode != codes.OK {
				return
			}
			require.Equal(t, int64(42), resp.GetMessage().GetId())
			require.Equal(t, "msg_x", resp.GetMessage().GetPublicId())
			require.Equal(t, int32(2), resp.GetMessage().GetDeliveryCount())
			require.False(t, resp.GetDuplicate())
		})
	}
}

func TestGetMessage(t *testing.T) {
	now := time.Now()
	reason := "http_404"
	f := newFixture(t)
	f.messages.EXPECT().GetMessage(gomock.Any(), scope, int64(42)).Return(
		&models.Message{ID: 42, PublicID: "msg_x", AcceptedAt: now},
		&models.Application{ID: 7, EnvironmentID: 2, EnvironmentKind: "production"},
		[]models.Delivery{{ID: 1, MessageID: 42, EndpointID: 3, State: models.DeliveryRetryScheduled, AttemptCount: 1, DueAt: &now, DeadlineAt: now, CreatedAt: now, FailureReason: &reason}}, nil)
	resp, err := f.client.GetMessage(scoped(t), &deliveryv1.GetMessageRequest{MessageId: 42})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetMessage().GetDeliveryCount())
	require.Equal(t, int64(2), resp.GetMessage().GetEnvironmentId())
	require.Equal(t, "production", resp.GetMessage().GetEnvironmentKind())
	require.Len(t, resp.GetDeliveries(), 1)
	require.Equal(t, deliveryv1.DeliveryState_DELIVERY_STATE_RETRY_SCHEDULED, resp.GetDeliveries()[0].GetState())
	require.Equal(t, "http_404", resp.GetDeliveries()[0].GetFailureReason())

	f.messages.EXPECT().GetMessage(gomock.Any(), scope, int64(-1)).Return(nil, nil, nil, apperr.NewNotFound("message", nil))
	_, err = f.client.GetMessage(scoped(t), &deliveryv1.GetMessageRequest{MessageId: -1})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestListMessages(t *testing.T) {
	now := time.Now()
	f := newFixture(t)
	f.messages.EXPECT().ListMessages(gomock.Any(), scope, gomock.Any()).DoAndReturn(func(_ context.Context, _ services.Scope, o repos.ListMessagesOpts) ([]models.MessageForList, int, error) {
		require.Equal(t, int64(7), o.ApplicationID)
		require.Equal(t, "order.paid", o.EventType)
		require.Equal(t, 20, o.Limit)
		require.NotNil(t, o.AcceptedAfter)
		require.Nil(t, o.AcceptedBefore)
		m := models.MessageForList{ID: 1, PublicID: "msg_x", ApplicationID: 7, EnvironmentID: 2, EnvironmentKind: "production", EventType: "order.paid", AcceptedAt: now, PayloadExpiresAt: now}
		m.Succeeded, m.Failed = 2, 1
		return []models.MessageForList{m}, 1, nil
	})
	resp, err := f.client.ListMessages(scoped(t), &deliveryv1.ListMessagesRequest{ApplicationId: 7, EventType: "order.paid", Limit: 20, AcceptedAfter: timestamppb.New(now)})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetTotal())
	require.Len(t, resp.GetMessages(), 1)
	require.Equal(t, int32(3), resp.GetMessages()[0].GetMessage().GetDeliveryCount())
	require.Equal(t, int32(2), resp.GetMessages()[0].GetCounts().GetSucceeded())
	require.Equal(t, "production", resp.GetMessages()[0].GetMessage().GetEnvironmentKind())

	f.messages.EXPECT().ListMessages(gomock.Any(), scope, gomock.Any()).Return(nil, 0, apperr.NewValidation(map[string]string{"limit": "x"}))
	_, err = f.client.ListMessages(scoped(t), &deliveryv1.ListMessagesRequest{ApplicationId: 7})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListAttempts(t *testing.T) {
	now := time.Now()
	code := int32(503)
	f := newFixture(t)
	f.messages.EXPECT().ListAttempts(gomock.Any(), scope, int64(5)).Return([]models.Attempt{{ID: 1, DeliveryID: 5, Number: 1, StartedAt: now, CompletedAt: &now, StatusCode: &code, LatencyMS: 12, SecretVersion: 1, EndpointRevision: 1, RequestURL: "https://r"}}, nil)
	resp, err := f.client.ListAttempts(scoped(t), &deliveryv1.ListAttemptsRequest{DeliveryId: 5})
	require.NoError(t, err)
	require.Len(t, resp.GetAttempts(), 1)
	require.Equal(t, int32(503), resp.GetAttempts()[0].GetStatusCode())
	require.Equal(t, "https://r", resp.GetAttempts()[0].GetRequestUrl())
}

func TestUpdateEndpoint(t *testing.T) {
	now := time.Now()
	f := newFixture(t)
	url := "https://new.example.com/hooks"
	f.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), scope, gomock.Any()).DoAndReturn(func(_ context.Context, _ services.Scope, in services.UpdateEndpointInput) (*models.Endpoint, error) {
		require.Equal(t, int64(3), in.EndpointID)
		require.Equal(t, url, *in.URL)
		require.Nil(t, in.Description)
		require.Equal(t, []string{}, *in.EventTypes, "an empty list is sent as empty, not nil")
		return &models.Endpoint{ID: 3, URL: url, Revision: 2, EventTypes: []string{}, CreatedAt: now}, nil
	})
	resp, err := f.client.UpdateEndpoint(scoped(t), &deliveryv1.UpdateEndpointRequest{EndpointId: 3, Url: &url, EventTypes: &deliveryv1.EventTypeList{}})
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetEndpoint().GetRevision())

	f.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), scope, gomock.Any()).DoAndReturn(func(_ context.Context, _ services.Scope, in services.UpdateEndpointInput) (*models.Endpoint, error) {
		require.Nil(t, in.EventTypes, "absent list means keep the subscriptions")
		return nil, apperr.NewConflict("endpoint is archived", nil)
	})
	_, err = f.client.UpdateEndpoint(scoped(t), &deliveryv1.UpdateEndpointRequest{EndpointId: 3, Url: &url})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestRotateEndpointSecret(t *testing.T) {
	now := time.Now()
	f := newFixture(t)
	f.endpoints.EXPECT().RotateEndpointSecret(gomock.Any(), scope, int64(3), 24*time.Hour).Return(&models.Endpoint{ID: 3, CreatedAt: now}, "whsec_new", int32(2), now.Add(24*time.Hour), nil)
	resp, err := f.client.RotateEndpointSecret(scoped(t), &deliveryv1.RotateEndpointSecretRequest{EndpointId: 3, RetireIn: durationpb.New(24 * time.Hour)})
	require.NoError(t, err)
	require.Equal(t, "whsec_new", resp.GetSigningSecret())
	require.Equal(t, int32(2), resp.GetSecretVersion())
	require.WithinDuration(t, now.Add(24*time.Hour), resp.GetRetireAt().AsTime(), time.Second)

	f.endpoints.EXPECT().RotateEndpointSecret(gomock.Any(), scope, int64(3), time.Duration(0)).Return(nil, "", int32(0), time.Time{}, apperr.NewConflict("endpoint is archived", nil))
	_, err = f.client.RotateEndpointSecret(scoped(t), &deliveryv1.RotateEndpointSecretRequest{EndpointId: 3})
	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestResumeEndpoint(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "resumed", wantCode: codes.OK},
		{name: "archived", err: apperr.NewConflict("endpoint is archived", nil), wantCode: codes.AlreadyExists},
		{name: "not found", err: apperr.NewNotFound("endpoint", nil), wantCode: codes.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var e *models.Endpoint
			if tt.err == nil {
				e = &models.Endpoint{ID: 3, CreatedAt: now}
			}
			f.endpoints.EXPECT().ResumeEndpoint(gomock.Any(), scope, int64(3)).Return(e, tt.err)
			resp, err := f.client.ResumeEndpoint(scoped(t), &deliveryv1.ResumeEndpointRequest{EndpointId: 3})
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				require.Nil(t, resp.GetEndpoint().PausedAt)
			}
		})
	}
}

func TestArchiveEndpoint(t *testing.T) {
	now := time.Now()
	f := newFixture(t)
	f.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), scope, int64(3)).Return(&models.Endpoint{ID: 3, ArchivedAt: &now, CreatedAt: now}, int64(2), nil)
	resp, err := f.client.ArchiveEndpoint(scoped(t), &deliveryv1.ArchiveEndpointRequest{EndpointId: 3})
	require.NoError(t, err)
	require.NotNil(t, resp.GetEndpoint().ArchivedAt)
	require.Equal(t, int64(2), resp.GetCancelledDeliveries())

	f.endpoints.EXPECT().ArchiveEndpoint(gomock.Any(), scope, int64(-1)).Return(nil, int64(0), apperr.NewNotFound("endpoint", nil))
	_, err = f.client.ArchiveEndpoint(scoped(t), &deliveryv1.ArchiveEndpointRequest{EndpointId: -1})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCancelDelivery(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "cancelled", wantCode: codes.OK},
		{name: "not found", err: apperr.NewNotFound("delivery", nil), wantCode: codes.NotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var d *models.Delivery
			if tt.err == nil {
				d = &models.Delivery{ID: 5, MessageID: 1, EndpointID: 2, State: models.DeliveryCancelled, DeadlineAt: time.Now()}
			}
			f.messages.EXPECT().CancelDelivery(gomock.Any(), scope, int64(5)).Return(d, tt.err)
			resp, err := f.client.CancelDelivery(scoped(t), &deliveryv1.CancelDeliveryRequest{DeliveryId: 5})
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				require.Equal(t, deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED, resp.GetDelivery().GetState())
			}
		})
	}
	t.Run("without workspace", func(t *testing.T) {
		_, err := newFixture(t).client.CancelDelivery(context.Background(), &deliveryv1.CancelDeliveryRequest{DeliveryId: 5})
		require.Equal(t, codes.Unauthenticated, status.Code(err))
	})
}

func TestReplayDelivery(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "replayed", wantCode: codes.OK},
		{name: "conflict", err: apperr.NewConflict("active", nil), wantCode: codes.AlreadyExists},
		{name: "validation", err: apperr.NewValidation(map[string]string{"Idempotency-Key": "required"}), wantCode: codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			var d *models.Delivery
			if tt.err == nil {
				original := int64(5)
				d = &models.Delivery{ID: 9, MessageID: 1, EndpointID: 2, State: models.DeliveryQueued, DeadlineAt: time.Now(), ReplayOfDeliveryID: &original}
			}
			f.messages.EXPECT().ReplayDelivery(gomock.Any(), scope, int64(5), "r1").Return(d, tt.err)
			resp, err := f.client.ReplayDelivery(scoped(t), &deliveryv1.ReplayDeliveryRequest{DeliveryId: 5, IdempotencyKey: "r1"})
			require.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				require.Equal(t, int64(9), resp.GetDelivery().GetId())
				require.Equal(t, int64(5), resp.GetDelivery().GetReplayOfDeliveryId())
			}
		})
	}
}
