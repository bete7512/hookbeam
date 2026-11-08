package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type healthFixture struct {
	repo   *repo_mocks.MockHealthRepo
	outbox *repo_mocks.MockOutboxRepo
	events []apievents.EndpointHealthChangedPayload
	now    time.Time
	svc    services.HealthService
}

func newHealth(t *testing.T) *healthFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &healthFixture{repo: repo_mocks.NewMockHealthRepo(ctrl), outbox: repo_mocks.NewMockOutboxRepo(ctrl), now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)}
	f.repo.EXPECT().WithTx(gomock.Any()).Return(f.repo).AnyTimes()
	f.outbox.EXPECT().WithTx(gomock.Any()).Return(f.outbox).AnyTimes()
	f.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
		require.Equal(t, apievents.EndpointHealthChanged, e.Type)
		var p apievents.EndpointHealthChangedPayload
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		f.events = append(f.events, p)
		return nil
	}).AnyTimes()
	var err error
	f.svc, err = services.NewHealthService(services.HealthServiceDeps{Health: f.repo, Outbox: f.outbox, Clock: func() time.Time { return f.now }, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return f
}

func TestNewHealthService(t *testing.T) {
	_, err := services.NewHealthService(services.HealthServiceDeps{})
	require.Error(t, err)
}

func TestHealthObserve(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	open := int64(9)
	row := func(failures int32, episode *int64) *models.EndpointHealth {
		return &models.EndpointHealth{EndpointID: 3, WorkspaceID: ws, ApplicationID: 7, Revision: 2, ConsecutiveFailures: failures, ObservedAt: at, OpenEpisodeID: episode}
	}
	tests := []struct {
		name      string
		failed    bool
		setup     func(f *healthFixture)
		wantEvent string // "" none
	}{
		{name: "first failures below the threshold open nothing", failed: true, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(row(2, nil), nil)
		}},
		{name: "the third consecutive failure opens an episode and announces it", failed: true, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(row(3, nil), nil)
			f.repo.EXPECT().OpenEpisode(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.HealthEpisode) error {
				require.Equal(t, int64(3), e.EndpointID)
				require.Equal(t, int32(2), e.Revision)
				require.Equal(t, int32(3), e.ConsecutiveFailures)
				require.Equal(t, at, e.OpenedAt)
				e.ID = 9
				return nil
			})
		}, wantEvent: models.HealthUnhealthy},
		{name: "a further failure continues the open episode without a new event", failed: true, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(row(4, &open), nil)
			f.repo.EXPECT().ContinueEpisode(gomock.Any(), int64(9), int32(4)).Return(nil)
		}},
		{name: "a success resolves the open episode and announces it", failed: false, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(row(0, &open), nil)
			f.repo.EXPECT().ResolveEpisode(gomock.Any(), int64(9), at).Return(nil)
		}, wantEvent: models.HealthHealthy},
		{name: "a success with nothing open is quiet", failed: false, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(row(0, nil), nil)
		}},
		{name: "an observation under an older revision is ignored", failed: true, setup: func(f *healthFixture) {
			f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("%w: stale", apperr.ErrNotFound))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHealth(t)
			tt.setup(f)
			err := f.svc.Observe(context.Background(), nil, models.HealthObservation{EndpointID: 3, WorkspaceID: ws, ApplicationID: 7, Revision: 2, Failed: tt.failed, ObservedAt: at})
			require.NoError(t, err)
			if tt.wantEvent == "" {
				require.Empty(t, f.events)
				return
			}
			require.Len(t, f.events, 1)
			require.Equal(t, tt.wantEvent, f.events[0].State)
			require.Equal(t, int64(9), f.events[0].EpisodeID)
			require.Equal(t, int64(3), f.events[0].EndpointID)
			require.Equal(t, int32(2), f.events[0].Revision)
			require.Equal(t, at, f.events[0].OccurredAt)
		})
	}
	t.Run("a repo failure surfaces", func(t *testing.T) {
		f := newHealth(t)
		f.repo.EXPECT().ObserveEndpointHealth(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
		require.Error(t, f.svc.Observe(context.Background(), nil, models.HealthObservation{EndpointID: 3, Failed: true}))
	})
}

func TestHealthStatus(t *testing.T) {
	open := int64(9)
	tests := []struct {
		name     string
		row      *models.EndpointHealth
		err      error
		age      time.Duration
		want     models.HealthStatus
		wantTime bool
	}{
		{name: "never attempted is unknown", err: fmt.Errorf("%w: none", apperr.ErrNotFound), want: models.HealthStatus{State: models.HealthUnknown}},
		{name: "recent success is healthy", row: &models.EndpointHealth{ConsecutiveFailures: 0}, age: time.Minute, want: models.HealthStatus{State: models.HealthHealthy}, wantTime: true},
		{name: "open episode is unhealthy", row: &models.EndpointHealth{ConsecutiveFailures: 5, OpenEpisodeID: &open}, age: time.Minute, want: models.HealthStatus{State: models.HealthUnhealthy, ConsecutiveFailures: 5}, wantTime: true},
		{name: "fifteen minutes of silence is stale", row: &models.EndpointHealth{}, age: services.HealthStaleAfter + time.Second, want: models.HealthStatus{State: models.HealthHealthy, Stale: true}, wantTime: true},
		{name: "exactly at the window is not stale yet", row: &models.EndpointHealth{}, age: services.HealthStaleAfter, want: models.HealthStatus{State: models.HealthHealthy}, wantTime: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHealth(t)
			if tt.row != nil {
				tt.row.ObservedAt = f.now.Add(-tt.age)
			}
			f.repo.EXPECT().GetEndpointHealth(gomock.Any(), ws, int64(3)).Return(tt.row, tt.err)
			got, err := f.svc.Status(context.Background(), ws, 3)
			require.NoError(t, err)
			require.Equal(t, tt.want.State, got.State)
			require.Equal(t, tt.want.Stale, got.Stale)
			require.Equal(t, tt.want.ConsecutiveFailures, got.ConsecutiveFailures)
			if tt.wantTime {
				require.Equal(t, tt.row.ObservedAt, *got.ObservedAt)
			} else {
				require.Nil(t, got.ObservedAt)
			}
		})
	}
}

func TestHealthEpisode(t *testing.T) {
	f := newHealth(t)
	f.repo.EXPECT().GetEpisode(gomock.Any(), ws, int64(9)).Return(&models.HealthEpisode{ID: 9, EndpointID: 3}, nil)
	e, err := f.svc.Episode(context.Background(), scope, 9)
	require.NoError(t, err)
	require.Equal(t, int64(3), e.EndpointID)

	f.repo.EXPECT().GetEpisode(gomock.Any(), ws, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
	_, err = f.svc.Episode(context.Background(), scope, -1)
	require.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = f.svc.Episode(context.Background(), services.Scope{}, 9)
	require.ErrorIs(t, err, apperr.ErrUnauthorized)
}
