package services

//go:generate mockgen -source=endpoint_health.go -destination=service_mocks/mock_endpoint_health.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// EndpointHealthService turns delivery state events into the per-endpoint health projection.
type EndpointHealthService interface {
	// Observe records one delivery state event; events that made no network request only refresh the last outcome.
	Observe(ctx context.Context, p apievents.DeliveryStatePayload) error
	// Get returns the health row of an endpoint in the actor's workspace.
	Get(ctx context.Context, actor Actor, endpointID int64) (*models.EndpointHealth, error)
}

// EndpointHealthServiceDeps are the dependencies; all are required.
type EndpointHealthServiceDeps struct {
	Health repos.EndpointHealthRepo
	Logger *slog.Logger
}

type endpointHealthService struct {
	deps EndpointHealthServiceDeps
}

// NewEndpointHealthService returns an EndpointHealthService or an error if a dependency is missing.
func NewEndpointHealthService(deps EndpointHealthServiceDeps) (EndpointHealthService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &endpointHealthService{deps: deps}, nil
}

// Reasons that end or defer a delivery without the receiver having refused an attempt.
const (
	reasonDeadlineExceeded = "deadline_exceeded"
	reasonUnknown          = "unknown"
)

func (s *endpointHealthService) Observe(ctx context.Context, p apievents.DeliveryStatePayload) error {
	if p.EndpointID <= 0 || p.WorkspaceID == "" || p.DeliveryID <= 0 || p.State == "" || p.OccurredAt.IsZero() {
		return apperr.NewBadRequest("delivery state event is incomplete", nil)
	}
	o := models.HealthObservation{
		EndpointID: p.EndpointID, WorkspaceID: p.WorkspaceID, ApplicationID: p.ApplicationID, DeliveryID: p.DeliveryID,
		Attempt: p.Attempt, State: p.State, Reason: p.Reason, StatusCode: p.StatusCode, ObservedAt: p.OccurredAt,
		CountsAsFailed: countsAsFailed(p),
	}
	if err := s.deps.Health.ObserveEndpointHealth(ctx, o); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

// countsAsFailed is true for an attempt the receiver did not accept: a retry or a terminal failure after a
// real request. Cancellations, deadlines that passed while waiting and lost attempts say nothing about the receiver.
func countsAsFailed(p apievents.DeliveryStatePayload) bool {
	if p.Attempt <= 0 || (p.State != apievents.DeliveryFailed[len("delivery."):] && p.State != apievents.DeliveryRetryScheduled[len("delivery."):]) {
		return false
	}
	if p.Reason != nil && (*p.Reason == reasonDeadlineExceeded || *p.Reason == reasonUnknown) {
		return false
	}
	return true
}

func (s *endpointHealthService) Get(ctx context.Context, actor Actor, endpointID int64) (*models.EndpointHealth, error) {
	if actor.WorkspaceID == "" {
		return nil, apperr.NewForbidden("select a workspace", nil)
	}
	h, err := s.deps.Health.GetEndpointHealth(ctx, actor.WorkspaceID, endpointID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil, apperr.NewNotFound("endpoint health", err)
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return h, nil
}
