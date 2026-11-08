package services

//go:generate mockgen -source=limits.go -destination=service_mocks/mock_limits.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Limits are the quotas the data plane enforces; see config.Limits for the defaults.
type Limits struct {
	EndpointsPerApplication int
	FanoutPerMessage        int
	BacklogPerWorkspace     int           // non-terminal deliveries a workspace may hold
	BacklogRetryAfter       time.Duration // what a submission over the backlog is told to wait
}

// Valid reports whether every limit is positive.
func (l Limits) Valid() bool {
	return l.EndpointsPerApplication > 0 && l.FanoutPerMessage > 0 && l.BacklogPerWorkspace > 0 && l.BacklogRetryAfter > 0
}

// LimitService reports a workspace's quotas and current usage.
type LimitService interface {
	Usage(ctx context.Context, scope Scope) (*models.WorkspaceLimits, error)
}

// LimitServiceDeps are the dependencies; all are required.
type LimitServiceDeps struct {
	Endpoints  repos.EndpointRepo
	Deliveries repos.DeliveryRepo
	Limits     Limits
	Logger     *slog.Logger
}

type limitService struct {
	deps LimitServiceDeps
}

// NewLimitService returns a LimitService or an error if a dependency is missing.
func NewLimitService(deps LimitServiceDeps) (LimitService, error) {
	if deps.Endpoints == nil || deps.Deliveries == nil || !deps.Limits.Valid() || deps.Logger == nil {
		return nil, errors.New("services: Endpoints, Deliveries, positive Limits and Logger are required")
	}
	return &limitService{deps: deps}, nil
}

func (s *limitService) Usage(ctx context.Context, scope Scope) (*models.WorkspaceLimits, error) {
	if scope.WorkspaceID == "" {
		return nil, apperr.NewUnauthorized("a workspace is required", nil)
	}
	backlog, err := s.deps.Deliveries.CountActiveDeliveries(ctx, scope.WorkspaceID, s.deps.Limits.BacklogPerWorkspace)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	apps, err := s.deps.Endpoints.ListEndpointCounts(ctx, scope.WorkspaceID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	if apps == nil {
		apps = []models.ApplicationEndpointCount{}
	}
	return &models.WorkspaceLimits{
		EndpointsPerApplication: s.deps.Limits.EndpointsPerApplication, FanoutPerMessage: s.deps.Limits.FanoutPerMessage, BacklogPerWorkspace: s.deps.Limits.BacklogPerWorkspace,
		Backlog: backlog, Applications: apps,
	}, nil
}
