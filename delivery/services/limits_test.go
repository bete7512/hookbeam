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
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

var testRetention = models.Retention{Payload: 168 * time.Hour, IdempotencyWindow: 168 * time.Hour, Audit: 2160 * time.Hour}

func TestLimitServiceUsage(t *testing.T) {
	ctrl := gomock.NewController(t)
	endpoints, deliveries := repo_mocks.NewMockEndpointRepo(ctrl), repo_mocks.NewMockDeliveryRepo(ctrl)
	svc, err := services.NewLimitService(services.LimitServiceDeps{Endpoints: endpoints, Deliveries: deliveries, Limits: testLimits, Retention: testRetention, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	_, err = services.NewLimitService(services.LimitServiceDeps{Endpoints: endpoints, Deliveries: deliveries, Logger: slog.New(slog.DiscardHandler)})
	require.Error(t, err, "zero limits are refused")

	deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(42, nil)
	endpoints.EXPECT().ListEndpointCounts(gomock.Any(), ws).Return([]models.ApplicationEndpointCount{{ApplicationID: 7, Name: "Customer A", Endpoints: 3}}, nil)
	u, err := svc.Usage(context.Background(), scope)
	require.NoError(t, err)
	require.Equal(t, 100, u.BacklogPerWorkspace)
	require.Equal(t, 42, u.Backlog)
	require.Len(t, u.Applications, 1)
	require.Equal(t, int32(3), u.Applications[0].Endpoints)

	deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, nil)
	endpoints.EXPECT().ListEndpointCounts(gomock.Any(), ws).Return(nil, nil)
	u, err = svc.Usage(context.Background(), scope)
	require.NoError(t, err)
	require.NotNil(t, u.Applications, "an empty workspace lists no applications, not null")

	deliveries.EXPECT().CountActiveDeliveries(gomock.Any(), ws, 100).Return(0, errors.New("boom"))
	_, err = svc.Usage(context.Background(), scope)
	require.ErrorIs(t, err, apperr.ErrInternal)
	_, err = svc.Usage(context.Background(), services.Scope{})
	require.ErrorIs(t, err, apperr.ErrUnauthorized)
}
