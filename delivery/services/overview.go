package services

//go:generate mockgen -source=overview.go -destination=service_mocks/mock_overview.go -package=service_mocks

import (
	"context"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// OverviewWindow is how far back the overview's volume counts look.
const OverviewWindow = 24 * time.Hour

// OverviewService reads an environment's overview tiles.
type OverviewService interface {
	Get(ctx context.Context, scope Scope, environmentID int64) (*models.Overview, error)
}

// OverviewServiceDeps are the dependencies; Clock defaults to time.Now.
type OverviewServiceDeps struct {
	Environments repos.EnvironmentRepo
	Overview     repos.OverviewRepo
	Clock        func() time.Time `depcheck:"optional"`
	Logger       *slog.Logger
}

type overviewService struct {
	deps OverviewServiceDeps
}

// NewOverviewService returns an OverviewService or an error if a dependency is missing.
func NewOverviewService(deps OverviewServiceDeps) (OverviewService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &overviewService{deps: deps}, nil
}

func (s *overviewService) Get(ctx context.Context, scope Scope, environmentID int64) (*models.Overview, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	if _, err := s.deps.Environments.GetEnvironment(ctx, scope.WorkspaceID, environmentID); err != nil {
		return nil, endpointError("environment", err)
	}
	now := s.deps.Clock()
	o, err := s.deps.Overview.GetOverview(ctx, scope.WorkspaceID, environmentID, now.Add(-OverviewWindow))
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	o.ComputedAt = now
	return o, nil
}
