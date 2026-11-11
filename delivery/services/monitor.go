package services

//go:generate mockgen -source=monitor.go -destination=service_mocks/mock_monitor.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// MonitorService reads the shape of the queue for the gauges; every process registers them, so the numbers
// keep arriving when the worker is the thing that died.
type MonitorService interface {
	// OutboxLag is the age of the oldest unpublished event, or zero with an empty outbox.
	OutboxLag(ctx context.Context) (time.Duration, error)
	// Backlog is the age of the oldest due, unclaimed delivery and the deliveries under a lease.
	Backlog(ctx context.Context) (BacklogStats, error)
	// RegisterGauges registers the gauges on the process's meter; each export reads them.
	RegisterGauges() error
}

// BacklogStats is what the dispatch gauges observe.
type BacklogStats struct {
	OldestDueAge time.Duration
	InFlight     int
}

// MonitorServiceDeps are the dependencies; all are required.
type MonitorServiceDeps struct {
	Outbox     repos.OutboxRepo
	Deliveries repos.DeliveryRepo
	Logger     *slog.Logger
}

type monitorService struct {
	deps MonitorServiceDeps
}

// NewMonitorService returns a MonitorService or an error if a dependency is missing.
func NewMonitorService(deps MonitorServiceDeps) (MonitorService, error) {
	if deps.Outbox == nil || deps.Deliveries == nil || deps.Logger == nil {
		return nil, errors.New("services: Outbox, Deliveries and Logger are required")
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

func (s *monitorService) Backlog(ctx context.Context) (BacklogStats, error) {
	oldest, err := s.deps.Deliveries.OldestDueAt(ctx)
	if err != nil {
		return BacklogStats{}, apperr.NewInternal(err)
	}
	inFlight, err := s.deps.Deliveries.CountInFlight(ctx)
	if err != nil {
		return BacklogStats{}, apperr.NewInternal(err)
	}
	var age time.Duration
	if oldest != nil {
		age = max(0, time.Since(*oldest))
	}
	return BacklogStats{OldestDueAge: age, InFlight: inFlight}, nil
}

func (s *monitorService) RegisterGauges() error {
	if err := telemetry.Gauge("hookbeam.outbox.oldest_age", "age in seconds of the oldest unpublished outbox event", func(ctx context.Context) (float64, error) {
		lag, err := s.OutboxLag(ctx)
		return lag.Seconds(), err
	}); err != nil {
		return err
	}
	if err := telemetry.Gauge("hookbeam.deliveries.oldest_due_age", "age in seconds of the oldest delivery that is due and not yet claimed", func(ctx context.Context) (float64, error) {
		b, err := s.Backlog(ctx)
		return b.OldestDueAge.Seconds(), err
	}); err != nil {
		return err
	}
	return telemetry.Gauge("hookbeam.deliveries.in_flight", "deliveries under a lease right now", func(ctx context.Context) (float64, error) {
		b, err := s.Backlog(ctx)
		return float64(b.InFlight), err
	})
}
