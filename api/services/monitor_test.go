package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func newMonitor(t *testing.T) (services.MonitorService, *repo_mocks.MockOutboxRepo) {
	t.Helper()
	outbox := repo_mocks.NewMockOutboxRepo(gomock.NewController(t))
	svc, err := services.NewMonitorService(services.MonitorServiceDeps{Outbox: outbox, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, outbox
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
			svc, outbox := newMonitor(t)
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

func TestMonitorRegisterGauges(t *testing.T) {
	svc, _ := newMonitor(t)
	require.NoError(t, svc.RegisterGauges())
}
