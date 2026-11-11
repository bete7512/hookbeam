package services_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// viewer is a user the fixture's authorizer refuses everything but metadata.
const viewer = "u_viewer"

var (
	developer = services.Actor{UserID: "u_ada", WorkspaceID: ws}
	viewerAct = services.Actor{UserID: viewer, WorkspaceID: ws}
	keyActor  = services.Actor{WorkspaceID: ws}
	devScope  = services.Scope{WorkspaceID: ws, ActorID: "u_ada"}
	keyScope  = services.Scope{WorkspaceID: ws}
)

// newDelivery returns the service over a DataPlane mock and an authorizer that lets every user read metadata and
// refuses the viewer everything else.
func newDelivery(t *testing.T) (services.DeliveryService, *service_mocks.MockDataPlane) {
	t.Helper()
	ctrl := gomock.NewController(t)
	dp := service_mocks.NewMockDataPlane(ctrl)
	authz := service_mocks.NewMockAuthorizer(ctrl)
	authz.EXPECT().Can(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a services.Actor, c services.Capability) error {
		if a.WorkspaceID == "" {
			return apperr.NewForbidden("select a workspace", nil)
		}
		if a.UserID == viewer && c != services.CapReadMetadata {
			return apperr.NewForbidden("a viewer cannot do that", nil)
		}
		return nil
	}).AnyTimes()
	svc, err := services.NewDeliveryService(services.DeliveryServiceDeps{DataPlane: dp, Authz: authz, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, dp
}

func TestNewDeliveryService(t *testing.T) {
	_, err := services.NewDeliveryService(services.DeliveryServiceDeps{})
	require.Error(t, err)
}

// TestDeliveryAuthorization checks that the role matrix runs before the data plane: a refused actor never reaches
// the mock, an allowed one reaches it under their scope.
func TestDeliveryAuthorization(t *testing.T) {
	env := int64(2)
	tests := []struct {
		name   string
		expect func(dp *service_mocks.MockDataPlane) // the call the data plane sees when allowed
		call   func(svc services.DeliveryService, actor services.Actor) error
	}{
		{
			name: "create application",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().CreateApplication(gomock.Any(), devScope, nil, "A").Return(&models.Application{ID: 1}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.CreateApplication(context.Background(), a, "A")
				return err
			},
		},
		{
			name: "create application in an environment",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().CreateApplication(gomock.Any(), devScope, &env, "A").Return(&models.Application{ID: 1}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.CreateApplicationIn(context.Background(), a, env, "A")
				return err
			},
		},
		{
			name: "update application",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().UpdateApplication(gomock.Any(), devScope, int64(7), gomock.Any()).Return(&models.Application{ID: 7}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.UpdateApplication(context.Background(), a, 7, services.UpdateApplicationInput{})
				return err
			},
		},
		{
			name: "archive application",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ArchiveApplication(gomock.Any(), devScope, int64(7)).Return(&models.Application{ID: 7}, int64(1), int64(2), nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, _, _, err := svc.ArchiveApplication(context.Background(), a, 7)
				return err
			},
		},
		{
			name: "create environment",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().CreateEnvironment(gomock.Any(), devScope, "prod", "production").Return(&models.Environment{ID: 2}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.CreateEnvironment(context.Background(), a, "prod", "production")
				return err
			},
		},
		{
			name: "create endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().CreateEndpoint(gomock.Any(), devScope, gomock.Any()).Return(&models.Endpoint{ID: 3}, "whsec_x", int32(1), nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, _, _, err := svc.CreateEndpoint(context.Background(), a, services.CreateEndpointInput{ApplicationID: 7, URL: "https://r"})
				return err
			},
		},
		{
			name: "pause endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().PauseEndpoint(gomock.Any(), devScope, int64(3)).Return(&models.Endpoint{ID: 3}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.PauseEndpoint(context.Background(), a, 3)
				return err
			},
		},
		{
			name: "resume endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ResumeEndpoint(gomock.Any(), devScope, int64(3)).Return(&models.Endpoint{ID: 3}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ResumeEndpoint(context.Background(), a, 3)
				return err
			},
		},
		{
			name: "update endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().UpdateEndpoint(gomock.Any(), devScope, gomock.Any()).Return(&models.Endpoint{ID: 3}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.UpdateEndpoint(context.Background(), a, services.UpdateEndpointInput{EndpointID: 3})
				return err
			},
		},
		{
			name: "rotate secret",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().RotateEndpointSecret(gomock.Any(), devScope, int64(3), gomock.Any()).Return(&models.SecretRotated{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.RotateEndpointSecret(context.Background(), a, 3, 0)
				return err
			},
		},
		{
			name: "archive endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ArchiveEndpoint(gomock.Any(), devScope, int64(3)).Return(&models.Endpoint{ID: 3}, int64(0), nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, _, err := svc.ArchiveEndpoint(context.Background(), a, 3)
				return err
			},
		},
		{
			name: "send test message",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().SendTestMessage(gomock.Any(), devScope, int64(3)).Return(&models.Message{ID: 42}, &models.Delivery{ID: 9}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, _, err := svc.SendTestMessage(context.Background(), a, 3)
				return err
			},
		},
		{
			name: "cancel delivery",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().CancelDelivery(gomock.Any(), devScope, int64(5)).Return(&models.Delivery{ID: 5}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.CancelDelivery(context.Background(), a, 5)
				return err
			},
		},
		{
			name: "replay delivery",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ReplayDelivery(gomock.Any(), devScope, int64(5), "r1").Return(&models.Delivery{ID: 9}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ReplayDelivery(context.Background(), a, 5, "r1")
				return err
			},
		},
		{
			name: "read payload",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetMessagePayload(gomock.Any(), devScope, int64(42)).Return(&models.MessagePayload{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetMessagePayload(context.Background(), a, 42)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			tt.expect(dp)
			require.NoError(t, tt.call(svc, developer), "a developer is allowed")

			require.ErrorIs(t, tt.call(svc, viewerAct), apperr.ErrForbidden, "a viewer is refused before the data plane")
			require.ErrorIs(t, tt.call(svc, services.Actor{UserID: "u_ada"}), apperr.ErrForbidden, "no workspace is refused")
		})
	}
}

// TestDeliveryReads checks the metadata capability: every role, scoped by the actor.
func TestDeliveryReads(t *testing.T) {
	tests := []struct {
		name   string
		expect func(dp *service_mocks.MockDataPlane)
		call   func(svc services.DeliveryService, actor services.Actor) error
	}{
		{
			name: "get application",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetApplication(gomock.Any(), gomock.Any(), int64(7)).Return(&models.Application{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetApplication(context.Background(), a, 7)
				return err
			},
		},
		{
			name: "list applications",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ListApplications(gomock.Any(), gomock.Any(), int64(2)).Return(nil, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ListApplications(context.Background(), a, 2)
				return err
			},
		},
		{
			name: "list endpoints",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ListEndpoints(gomock.Any(), gomock.Any(), int64(7)).Return(nil, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ListEndpoints(context.Background(), a, 7)
				return err
			},
		},
		{
			name: "get endpoint",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetEndpoint(gomock.Any(), gomock.Any(), int64(3)).Return(&models.Endpoint{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetEndpoint(context.Background(), a, 3)
				return err
			},
		},
		{
			name: "overview",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetOverview(gomock.Any(), gomock.Any(), int64(2)).Return(&models.Overview{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetOverview(context.Background(), a, 2)
				return err
			},
		},
		{
			name: "list environments",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ListEnvironments(gomock.Any(), gomock.Any()).Return(nil, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ListEnvironments(context.Background(), a)
				return err
			},
		},
		{
			name: "workspace limits",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetWorkspaceLimits(gomock.Any(), gomock.Any()).Return(&models.WorkspaceLimits{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetWorkspaceLimits(context.Background(), a)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			tt.expect(dp)
			require.NoError(t, tt.call(svc, viewerAct), "a viewer reads metadata")
			require.ErrorIs(t, tt.call(svc, services.Actor{UserID: viewer}), apperr.ErrForbidden)
		})
	}
}

// TestDeliveryKeyActor checks that an API key (no user) skips the role matrix, since the middleware already
// enforced its scopes, and still needs a workspace.
func TestDeliveryKeyActor(t *testing.T) {
	tests := []struct {
		name   string
		expect func(dp *service_mocks.MockDataPlane)
		call   func(svc services.DeliveryService, actor services.Actor) error
	}{
		{
			name: "replay",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ReplayDelivery(gomock.Any(), keyScope, int64(5), "r1").Return(&models.Delivery{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.ReplayDelivery(context.Background(), a, 5, "r1")
				return err
			},
		},
		{
			name: "read payload",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetMessagePayload(gomock.Any(), keyScope, int64(42)).Return(&models.MessagePayload{}, nil)
			},
			call: func(svc services.DeliveryService, a services.Actor) error {
				_, err := svc.GetMessagePayload(context.Background(), a, 42)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			tt.expect(dp)
			require.NoError(t, tt.call(svc, keyActor), "a key passed the scope gate already")

			err := tt.call(svc, services.Actor{})
			var appErr *apperr.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 403, appErr.Code)
			require.Equal(t, "select a workspace", appErr.Message)
		})
	}
}

func TestDeliverySubmitMessage(t *testing.T) {
	payload := []byte(`{"type":"order.shipped","external_id":"order-817","data":{"n":1}}`)
	external := "order-817"
	tests := []struct {
		name        string
		workspaceID string
		payload     []byte
		want        *services.AcceptMessageInput // nil: the data plane is never called
		wantCode    int
		wantField   string
	}{
		{name: "type and external id are read, bytes forwarded untouched", workspaceID: ws, payload: payload, want: &services.AcceptMessageInput{ApplicationID: 7, EventType: "order.shipped", IdempotencyKey: "k1", SenderEventID: &external, Payload: payload}},
		{name: "without external id", workspaceID: ws, payload: []byte(`{"type":"a"}`), want: &services.AcceptMessageInput{ApplicationID: 7, EventType: "a", IdempotencyKey: "k1", Payload: []byte(`{"type":"a"}`)}},
		{name: "missing type", workspaceID: ws, payload: []byte(`{"data":{}}`), wantCode: 422, wantField: "body"},
		{name: "not json", workspaceID: ws, payload: []byte(`not json`), wantCode: 422, wantField: "body"},
		{name: "no workspace", payload: payload, wantCode: 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			if tt.want != nil {
				dp.EXPECT().AcceptMessage(gomock.Any(), services.Scope{WorkspaceID: ws}, *tt.want).Return(&models.MessageAccepted{Message: models.Message{ID: 42}, Duplicate: true}, nil)
			}
			res, err := svc.SubmitMessage(context.Background(), tt.workspaceID, services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "k1", Payload: tt.payload})
			if tt.wantCode != 0 {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, tt.wantCode, appErr.Code)
				if tt.wantField != "" {
					require.Contains(t, appErr.Fields, tt.wantField)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(42), res.Message.ID)
			require.True(t, res.Duplicate)
		})
	}
}

// TestDeliveryListAttempts checks who gets response previews: the role matrix for a user, payloads:read for a key.
func TestDeliveryListAttempts(t *testing.T) {
	tests := []struct {
		name         string
		actor        services.Actor
		scopes       []string
		wantPreviews bool
		wantCode     int
	}{
		{name: "a developer asks for previews", actor: developer, wantPreviews: true},
		{name: "a viewer does not", actor: viewerAct},
		{name: "a key with payloads:read asks for previews", actor: keyActor, scopes: []string{"messages:read", "payloads:read"}, wantPreviews: true},
		{name: "a key without it does not", actor: keyActor, scopes: []string{"messages:read"}},
		{name: "no workspace", actor: services.Actor{UserID: "u_ada"}, wantCode: 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			if tt.wantCode == 0 {
				dp.EXPECT().ListAttempts(gomock.Any(), services.Scope{WorkspaceID: tt.actor.WorkspaceID, ActorID: tt.actor.UserID}, int64(5), tt.wantPreviews).Return([]models.Attempt{{ID: 1}}, nil)
			}
			got, err := svc.ListAttempts(context.Background(), tt.actor, services.ListAttemptsInput{DeliveryID: 5, KeyScopes: tt.scopes})
			if tt.wantCode != 0 {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, tt.wantCode, appErr.Code)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
		})
	}
}

// TestDeliveryWorkspaceRequired covers the routes that accept keys too: the workspace is the whole identity,
// so an empty one is 401 and nothing reaches the data plane.
func TestDeliveryWorkspaceRequired(t *testing.T) {
	tests := []struct {
		name   string
		expect func(dp *service_mocks.MockDataPlane)
		call   func(svc services.DeliveryService, workspaceID string) error
	}{
		{
			name: "get message",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetMessage(gomock.Any(), keyScope, int64(42)).Return(&models.Message{}, nil, nil)
			},
			call: func(svc services.DeliveryService, w string) error {
				_, _, err := svc.GetMessage(context.Background(), w, 42)
				return err
			},
		},
		{
			name: "list messages",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ListMessages(gomock.Any(), keyScope, gomock.Any()).Return(nil, 0, nil)
			},
			call: func(svc services.DeliveryService, w string) error {
				_, _, err := svc.ListMessages(context.Background(), w, services.ListMessagesInput{ApplicationID: 7})
				return err
			},
		},
		{
			name: "list audit events",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().ListAuditEvents(gomock.Any(), keyScope, gomock.Any()).Return(nil, 0, nil)
			},
			call: func(svc services.DeliveryService, w string) error {
				_, _, err := svc.ListAuditEvents(context.Background(), w, services.ListAuditInput{Limit: 20})
				return err
			},
		},
		{
			name: "get health episode",
			expect: func(dp *service_mocks.MockDataPlane) {
				dp.EXPECT().GetHealthEpisode(gomock.Any(), keyScope, int64(9)).Return(&models.HealthEpisode{}, &models.Endpoint{}, nil)
			},
			call: func(svc services.DeliveryService, w string) error {
				_, _, err := svc.GetHealthEpisode(context.Background(), w, 9)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, dp := newDelivery(t)
			tt.expect(dp)
			require.NoError(t, tt.call(svc, ws), "a workspace alone is enough: the scope gate already ran")
			require.ErrorIs(t, tt.call(svc, ""), apperr.ErrUnauthorized)
		})
	}
}

// TestDeliveryPassesErrorsThrough checks that a data plane error reaches the caller as it came: the client already
// mapped it to an AppError.
func TestDeliveryPassesErrorsThrough(t *testing.T) {
	svc, dp := newDelivery(t)
	dp.EXPECT().GetEndpoint(gomock.Any(), devScope, int64(-1)).Return(nil, apperr.NewNotFound("endpoint", nil))
	_, err := svc.GetEndpoint(context.Background(), developer, -1)
	require.ErrorIs(t, err, apperr.ErrNotFound)
}
