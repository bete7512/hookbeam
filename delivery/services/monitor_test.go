package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func newMonitor(t *testing.T) (services.MonitorService, *repo_mocks.MockOutboxRepo, *repo_mocks.MockDeliveryRepo) {
	t.Helper()
	ctrl := gomock.NewController(t)
	outbox := repo_mocks.NewMockOutboxRepo(ctrl)
	deliveries := repo_mocks.NewMockDeliveryRepo(ctrl)
	svc, err := services.NewMonitorService(services.MonitorServiceDeps{Outbox: outbox, Deliveries: deliveries, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, outbox, deliveries
}

func TestNewMonitorService(t *testing.T) {
	_, err := services.NewMonitorService(services.MonitorServiceDeps{})
	require.Error(t, err)
}

func TestMonitorOutboxLag(t *testing.T) {
	past := time.Now().Add(-90 * time.Second)
	tests := []struct {
		name    string
		at      *time.Time
		err     error
		wantMin time.Duration
		wantErr error
	}{
		{name: "oldest event ages", at: &past, wantMin: 90 * time.Second},
		{name: "empty outbox is zero", at: nil},
		{name: "repo error is internal", err: errors.New("db down"), wantErr: apperr.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, outbox, _ := newMonitor(t)
			outbox.EXPECT().OldestUnpublishedAt(gomock.Any()).Return(tt.at, tt.err)

			lag, err := svc.OutboxLag(context.Background())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.GreaterOrEqual(t, lag, tt.wantMin)
		})
	}
}

func TestMonitorBacklog(t *testing.T) {
	past := time.Now().Add(-30 * time.Second)
	tests := []struct {
		name     string
		oldest   *time.Time
		inFlight int
		err      error
		wantMin  time.Duration
		wantErr  error
	}{
		{name: "due work ages and leases are counted", oldest: &past, inFlight: 2, wantMin: 30 * time.Second},
		{name: "nothing due is zero", inFlight: 0},
		{name: "repo error is internal", err: errors.New("db down"), wantErr: apperr.ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, deliveries := newMonitor(t)
			deliveries.EXPECT().OldestDueAt(gomock.Any()).Return(tt.oldest, tt.err)
			if tt.err == nil {
				deliveries.EXPECT().CountInFlight(gomock.Any()).Return(tt.inFlight, nil)
			}

			b, err := svc.Backlog(context.Background())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.GreaterOrEqual(t, b.OldestDueAge, tt.wantMin)
			require.Equal(t, tt.inFlight, b.InFlight)
		})
	}
}

func TestMonitorRegisterGauges(t *testing.T) {
	svc, _, _ := newMonitor(t)
	require.NoError(t, svc.RegisterGauges())
}
