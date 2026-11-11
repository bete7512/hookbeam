package services

//go:generate mockgen -source=monitor.go -destination=service_mocks/mock_monitor.go -package=service_mocks

import (
	"context"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// MonitorService reads the outbox's shape for the gauges; the api process registers them too, so a stopped
// worker shows as lag rather than as silence.
type MonitorService interface {
	// OutboxLag is the age of the oldest unpublished event, or zero with an empty outbox.
	OutboxLag(ctx context.Context) (time.Duration, error)
	// RegisterGauges registers the gauges on the process's meter; each export reads them.
	RegisterGauges() error
}

// MonitorServiceDeps are the dependencies; all are required.
type MonitorServiceDeps struct {
	Outbox repos.OutboxRepo
	Logger *slog.Logger
}

type monitorService struct {
	deps MonitorServiceDeps
}

// NewMonitorService returns a MonitorService or an error if a dependency is missing.
func NewMonitorService(deps MonitorServiceDeps) (MonitorService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &monitorService{deps: deps}, nil
}

func (s *monitorService) OutboxLag(ctx context.Context) (time.Duration, error) {
	at, err := s.deps.Outbox.OldestUnpublishedAt(ctx)
	if err != nil {
		return 0, apperr.NewInternal(err)
	}
	if at == nil {
		return 0, nil
	}
	return max(0, time.Since(*at)), nil
}

func (s *monitorService) RegisterGauges() error {
	return telemetry.Gauge("hookbeam.outbox.oldest_age", "age in seconds of the oldest unpublished outbox event", func(ctx context.Context) (float64, error) {
		lag, err := s.OutboxLag(ctx)
		return lag.Seconds(), err
	})
}
