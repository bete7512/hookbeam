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

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/rpc"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/delivery/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

const ws = "ws_1"

var scope = services.Scope{WorkspaceID: ws, ActorID: "u_ada"}

type fixture struct {
	apps      *service_mocks.MockApplicationService
	endpoints *service_mocks.MockEndpointService
	client    deliveryv1.DeliveryServiceClient
}

// newFixture serves the real server with the standard interceptors over an in-memory connection.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{apps: service_mocks.NewMockApplicationService(ctrl), endpoints: service_mocks.NewMockEndpointService(ctrl)}
	srv, err := rpc.New(rpc.Deps{Applications: f.apps, Endpoints: f.endpoints, Logger: slog.New(slog.DiscardHandler)})
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

func TestCreateApplication(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		setup    func(f *fixture)
		noScope  bool
		wantCode codes.Code
	}{
		{name: "created", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, "Customer A").Return(&models.Application{ID: 1, WorkspaceID: ws, Name: "Customer A", CreatedBy: "u_ada", CreatedAt: now}, nil)
		}},
		{name: "validation", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, "Customer A").Return(nil, apperr.NewValidation(map[string]string{"name": "x"}))
		}, wantCode: codes.InvalidArgument},
		{name: "conflict", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, "Customer A").Return(nil, apperr.NewConflict("dup", nil))
		}, wantCode: codes.AlreadyExists},
		{name: "internal", setup: func(f *fixture) {
			f.apps.EXPECT().CreateApplication(gomock.Any(), scope, "Customer A").Return(nil, apperr.NewInternal(errors.New("boom")))
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

func TestUnimplemented(t *testing.T) {
	f := newFixture(t)
	ctx := scoped(t)
	_, err := f.client.AcceptMessage(ctx, &deliveryv1.AcceptMessageRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = f.client.GetMessage(ctx, &deliveryv1.GetMessageRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = f.client.ListAttempts(ctx, &deliveryv1.ListAttemptsRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
	_, err = f.client.ReplayDelivery(ctx, &deliveryv1.ReplayDeliveryRequest{})
	require.Equal(t, codes.Unimplemented, status.Code(err))
}
