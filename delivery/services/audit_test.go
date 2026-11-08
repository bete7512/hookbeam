package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func newAudit(t *testing.T) (services.AuditService, *repo_mocks.MockAuditRepo) {
	t.Helper()
	repo := repo_mocks.NewMockAuditRepo(gomock.NewController(t))
	svc, err := services.NewAuditService(services.AuditServiceDeps{Audit: repo, Retention: 90 * 24 * time.Hour, BatchSize: 2, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return svc, repo
}

func TestAuditServiceList(t *testing.T) {
	svc, repo := newAudit(t)
	repo.EXPECT().ListAuditEvents(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o repos.ListAuditOpts) ([]models.AuditEvent, error) {
		require.Equal(t, ws, o.WorkspaceID)
		require.Equal(t, "u_ada", o.ActorID)
		return []models.AuditEvent{{ID: 1}}, nil
	})
	repo.EXPECT().CountAuditEvents(gomock.Any(), gomock.Any()).Return(1, nil)
	items, total, err := svc.ListAuditEvents(context.Background(), scope, repos.ListAuditOpts{ActorID: "u_ada", Limit: 20})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 1, total)

	_, _, err = svc.ListAuditEvents(context.Background(), scope, repos.ListAuditOpts{Limit: 0})
	require.ErrorIs(t, err, apperr.ErrValidation)
	_, _, err = svc.ListAuditEvents(context.Background(), services.Scope{}, repos.ListAuditOpts{Limit: 20})
	require.ErrorIs(t, err, apperr.ErrUnauthorized)
}

func TestAuditServicePrune(t *testing.T) {
	svc, repo := newAudit(t)
	gomock.InOrder(
		repo.EXPECT().DeleteAuditEventsBefore(gomock.Any(), gomock.Any(), 2).Return(int64(2), nil),
		repo.EXPECT().DeleteAuditEventsBefore(gomock.Any(), gomock.Any(), 2).Return(int64(1), nil),
	)
	n, err := svc.Prune(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	repo.EXPECT().DeleteAuditEventsBefore(gomock.Any(), gomock.Any(), 2).Return(int64(0), errors.New("boom"))
	_, err = svc.Prune(context.Background())
	require.ErrorIs(t, err, apperr.ErrInternal)
}
