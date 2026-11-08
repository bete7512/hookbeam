package services_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestOverviewService(t *testing.T) {
	ctrl := gomock.NewController(t)
	envs, repo := repo_mocks.NewMockEnvironmentRepo(ctrl), repo_mocks.NewMockOverviewRepo(ctrl)
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	svc, err := services.NewOverviewService(services.OverviewServiceDeps{Environments: envs, Overview: repo, Clock: func() time.Time { return now }, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)

	envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(2)).Return(&models.Environment{ID: 2}, nil)
	repo.EXPECT().GetOverview(gomock.Any(), ws, int64(2), now.Add(-services.OverviewWindow)).Return(&models.Overview{Accepted: 10, Backlog: 3}, nil)
	o, err := svc.Get(context.Background(), scope, 2)
	require.NoError(t, err)
	require.Equal(t, int64(10), o.Accepted)
	require.Equal(t, now, o.ComputedAt, "the tiles say when they were read")

	envs.EXPECT().GetEnvironment(gomock.Any(), ws, int64(9)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
	_, err = svc.Get(context.Background(), scope, 9)
	require.ErrorIs(t, err, apperr.ErrNotFound)
	_, err = svc.Get(context.Background(), services.Scope{}, 2)
	require.ErrorIs(t, err, apperr.ErrUnauthorized)
}
