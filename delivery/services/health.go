package services

//go:generate mockgen -source=health.go -destination=service_mocks/mock_health.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	apievents "github.com/bete7512/hookbeam/delivery/events"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

const (
	// HealthFailureThreshold is how many consecutive failed attempts open an episode.
	HealthFailureThreshold = 3
	// HealthStaleAfter is how old the last observation may be before a read flags the health as stale.
	HealthStaleAfter = 15 * time.Minute
)

// HealthService keeps endpoint health episodes from attempt outcomes and describes them on reads.
type HealthService interface {
	// Observe applies one attempt's verdict inside the caller's transaction: the third consecutive failure opens
	// an episode, the next success resolves it, and either change is announced through the outbox in that
	// transaction. An observation older than the endpoint's observed revision is ignored.
	Observe(ctx context.Context, tx db.DBTX, o models.HealthObservation) error
	// Status describes an endpoint's health for a read; an endpoint never attempted is unknown.
	Status(ctx context.Context, workspaceID string, endpointID int64) (*models.HealthStatus, error)
}

// HealthServiceDeps are the dependencies; Clock defaults to time.Now.
type HealthServiceDeps struct {
	Health repos.HealthRepo
	Outbox repos.OutboxRepo
	Clock  func() time.Time
	Logger *slog.Logger
}

type healthService struct {
	deps HealthServiceDeps
}

// NewHealthService returns a HealthService or an error if a dependency is missing.
func NewHealthService(deps HealthServiceDeps) (HealthService, error) {
	if deps.Health == nil || deps.Outbox == nil || deps.Logger == nil {
		return nil, errors.New("services: Health, Outbox and Logger are required")
	}
	if deps.Clock == nil {
		deps.Clock = time.Now
	}
	return &healthService{deps: deps}, nil
}

func (s *healthService) Observe(ctx context.Context, tx db.DBTX, o models.HealthObservation) error {
	health := s.deps.Health.WithTx(tx)
	h, err := health.ObserveEndpointHealth(ctx, o)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil // an attempt planned under an older revision says nothing about the endpoint as it is now
	}
	if err != nil {
		return err
	}
	switch {
	case !o.Failed && h.OpenEpisodeID != nil:
		if err := health.ResolveEpisode(ctx, *h.OpenEpisodeID, o.ObservedAt); err != nil {
			return err
		}
		return s.announce(ctx, tx, h, *h.OpenEpisodeID, models.HealthHealthy, o.ObservedAt)
	case o.Failed && h.OpenEpisodeID != nil:
		return health.ContinueEpisode(ctx, *h.OpenEpisodeID, h.ConsecutiveFailures)
	case o.Failed && h.ConsecutiveFailures >= HealthFailureThreshold:
		episode := &models.HealthEpisode{EndpointID: h.EndpointID, WorkspaceID: h.WorkspaceID, Revision: h.Revision, ConsecutiveFailures: h.ConsecutiveFailures, OpenedAt: o.ObservedAt}
		if err := health.OpenEpisode(ctx, episode); err != nil {
			return err
		}
		return s.announce(ctx, tx, h, episode.ID, models.HealthUnhealthy, o.ObservedAt)
	}
	return nil
}

func (s *healthService) announce(ctx context.Context, tx db.DBTX, h *models.EndpointHealth, episodeID int64, state string, at time.Time) error {
	payload, err := json.Marshal(apievents.EndpointHealthChangedPayload{
		WorkspaceID: h.WorkspaceID, ApplicationID: h.ApplicationID, EndpointID: h.EndpointID, EpisodeID: episodeID,
		Revision: h.Revision, State: state, ConsecutiveFailures: h.ConsecutiveFailures, OccurredAt: at,
	})
	if err != nil {
		return err
	}
	return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.EndpointHealthChanged, Payload: payload})
}

func (s *healthService) Status(ctx context.Context, workspaceID string, endpointID int64) (*models.HealthStatus, error) {
	h, err := s.deps.Health.GetEndpointHealth(ctx, workspaceID, endpointID)
	if errors.Is(err, apperr.ErrNotFound) {
		return &models.HealthStatus{State: models.HealthUnknown}, nil
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	status := &models.HealthStatus{State: models.HealthHealthy, ObservedAt: &h.ObservedAt, ConsecutiveFailures: h.ConsecutiveFailures, Stale: s.deps.Clock().Sub(h.ObservedAt) > HealthStaleAfter}
	if h.OpenEpisodeID != nil {
		status.State = models.HealthUnhealthy
	}
	return status, nil
}
