package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func newHealth(t *testing.T) (services.EndpointHealthService, *repo_mocks.MockEndpointHealthRepo) {
	t.Helper()
	repo := repo_mocks.NewMockEndpointHealthRepo(gomock.NewController(t))
	svc, err := services.NewEndpointHealthService(services.EndpointHealthServiceDeps{Health: repo, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, repo
}

func TestNewEndpointHealthService(t *testing.T) {
	_, err := services.NewEndpointHealthService(services.EndpointHealthServiceDeps{})
	require.Error(t, err)
}

func TestEndpointHealthObserve(t *testing.T) {
	now := time.Now()
	str := func(v string) *string { return &v }
	base := apievents.DeliveryStatePayload{WorkspaceID: "ws_1", ApplicationID: 7, EndpointID: 3, MessageID: 42, DeliveryID: 5, Attempt: 1, OccurredAt: now}
	tests := []struct {
		name       string
		payload    func() apievents.DeliveryStatePayload
		wantFailed bool
		wantStatus int
	}{
		{name: "retry after a 503 counts as a failure", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason = "retry_scheduled", str("http_503")
			return p
		}, wantFailed: true},
		{name: "terminal 4xx counts as a failure", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason = "failed", str("http_410")
			return p
		}, wantFailed: true},
		{name: "attempts exhausted counts as a failure", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason, p.Attempt = "failed", str("attempts_exhausted"), 8
			return p
		}, wantFailed: true},
		{name: "success does not", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State = "succeeded"
			return p
		}},
		{name: "deadline passed while waiting says nothing about the receiver", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason = "failed", str("deadline_exceeded")
			return p
		}},
		{name: "lost attempt says nothing about the receiver", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason = "retry_scheduled", str("unknown")
			return p
		}},
		{name: "cancel before any attempt says nothing", payload: func() apievents.DeliveryStatePayload {
			p := base
			p.State, p.Reason, p.Attempt = "cancelled", str("cancelled"), 0
			return p
		}},
		{name: "incomplete event is rejected", payload: func() apievents.DeliveryStatePayload {
			return apievents.DeliveryStatePayload{State: "succeeded"}
		}, wantStatus: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newHealth(t)
			p := tt.payload()
			if tt.wantStatus == 0 {
				repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o models.HealthObservation) error {
					require.Equal(t, int64(3), o.EndpointID)
					require.Equal(t, p.State, o.State)
					require.Equal(t, tt.wantFailed, o.CountsAsFailed)
					require.Equal(t, now, o.ObservedAt)
					return nil
				})
			}

			err := svc.Observe(context.Background(), p)

			if tt.wantStatus != 0 {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, tt.wantStatus, appErr.Code)
				return
			}
			require.NoError(t, err)
		})
	}
	t.Run("repo failure", func(t *testing.T) {
		svc, repo := newHealth(t)
		repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		p := base
		p.State = "succeeded"
		require.ErrorIs(t, svc.Observe(context.Background(), p), apperr.ErrInternal)
	})
}

func TestEndpointHealthGet(t *testing.T) {
	svc, repo := newHealth(t)
	actor := services.Actor{UserID: "u", WorkspaceID: "ws_1"}
	repo.EXPECT().GetEndpointHealth(gomock.Any(), "ws_1", int64(3)).Return(&models.EndpointHealth{EndpointID: 3, ConsecutiveFailures: 2}, nil)
	h, err := svc.Get(context.Background(), actor, 3)
	require.NoError(t, err)
	require.Equal(t, int32(2), h.ConsecutiveFailures)

	repo.EXPECT().GetEndpointHealth(gomock.Any(), "ws_1", int64(-1)).Return(nil, apperr.ErrNotFound)
	_, err = svc.Get(context.Background(), actor, -1)
	require.ErrorIs(t, err, apperr.ErrNotFound)

	_, err = svc.Get(context.Background(), services.Actor{UserID: "u"}, 3)
	require.ErrorIs(t, err, apperr.ErrForbidden)
}
