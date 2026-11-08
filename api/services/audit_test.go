package services_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type auditFixture struct {
	repo     *repo_mocks.MockAuditRepo
	delivery *service_mocks.MockDeliveryService
	authz    *service_mocks.MockAuthorizer
	svc      services.AuditService
}

func newAuditService(t *testing.T) *auditFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &auditFixture{repo: repo_mocks.NewMockAuditRepo(ctrl), delivery: service_mocks.NewMockDeliveryService(ctrl), authz: service_mocks.NewMockAuthorizer(ctrl)}
	var err error
	f.svc, err = services.NewAuditService(services.AuditServiceDeps{Audit: f.repo, Delivery: f.delivery, Authz: f.authz, Retention: 90 * 24 * time.Hour, BatchSize: 2, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return f
}

func TestAuditServiceListMerges(t *testing.T) {
	actor := services.Actor{UserID: "u_ada", WorkspaceID: "ws_1"}
	now := time.Now()
	f := newAuditService(t)
	f.authz.EXPECT().Can(gomock.Any(), actor, services.CapReadMetadata).Return(nil)
	f.repo.EXPECT().ListAuditEvents(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o repos.ListAuditOpts) ([]models.AuditEvent, error) {
		require.Equal(t, "ws_1", o.WorkspaceID)
		require.Equal(t, 3, o.Limit, "offset + limit rows from each plane")
		return []models.AuditEvent{{ID: 10, Action: "api_key.create", OccurredAt: now.Add(-2 * time.Minute)}, {ID: 9, Action: "member.role", OccurredAt: now.Add(-4 * time.Minute)}}, nil
	})
	f.repo.EXPECT().CountAuditEvents(gomock.Any(), gomock.Any()).Return(2, nil)
	f.delivery.EXPECT().ListAuditEvents(gomock.Any(), "ws_1", gomock.Any()).Return([]models.AuditEvent{{ID: 5, Action: "endpoint.pause", OccurredAt: now.Add(-time.Minute)}, {ID: 4, Action: "delivery.cancel", OccurredAt: now.Add(-3 * time.Minute)}}, 2, nil)

	items, total, err := f.svc.List(context.Background(), actor, services.ListAuditInput{Limit: 2, Offset: 1})
	require.NoError(t, err)
	require.Equal(t, 4, total)
	require.Len(t, items, 2)
	require.Equal(t, "api_key.create", items[0].Action, "second newest overall")
	require.Equal(t, services.AuditSourceControl, items[0].Source)
	require.Equal(t, "delivery.cancel", items[1].Action)
	require.Equal(t, services.AuditSourceDelivery, items[1].Source)

	f.authz.EXPECT().Can(gomock.Any(), actor, services.CapReadMetadata).Return(nil)
	_, _, err = f.svc.List(context.Background(), actor, services.ListAuditInput{Limit: 101})
	require.ErrorIs(t, err, apperr.ErrValidation)
}

func TestAuditServiceRecordAndPrune(t *testing.T) {
	f := newAuditService(t)
	f.repo.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		require.Equal(t, "org.invite", e.Action)
		require.Equal(t, "ws_1", e.WorkspaceID)
		require.JSONEq(t, `{"email":"x@example.com"}`, string(e.Details))
		return nil
	})
	require.NoError(t, f.svc.Record(context.Background(), services.Actor{UserID: "u", WorkspaceID: "ws_1"}, "org.invite", "member", 0, map[string]any{"email": "x@example.com"}))

	gomock.InOrder(
		f.repo.EXPECT().DeleteAuditEventsBefore(gomock.Any(), gomock.Any(), 2).Return(int64(2), nil),
		f.repo.EXPECT().DeleteAuditEventsBefore(gomock.Any(), gomock.Any(), 2).Return(int64(0), nil),
	)
	n, err := f.svc.Prune(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
}
