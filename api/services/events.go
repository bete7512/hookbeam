package services

//go:generate mockgen -source=events.go -destination=service_mocks/mock_events.go -package=service_mocks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// EventService relays committed events and suppresses successfully recorded replays.
type EventService interface {
	// Publish drains unpublished events until empty or timed out, one batch per transaction.
	Publish(ctx context.Context) (int, error)
	// CleanOutbox deletes events published longer ago than the retention and returns how many it deleted.
	CleanOutbox(ctx context.Context) (int64, error)
	// ProcessOnce records eventID and runs fn; it reports false without running fn when the event was already processed.
	// An error from fn discards the record, so a redelivery runs fn again.
	// External effects and writes in fn are not in this transaction and may repeat after a failed commit.
	ProcessOnce(ctx context.Context, eventID int64, fn func(ctx context.Context) error) (bool, error)
}

// EventServiceDeps are the dependencies; PublishTimeout defaults to 30 seconds.
type EventServiceDeps struct {
	Tx             db.Transactor
	Outbox         repos.OutboxRepo
	Processed      repos.ProcessedEventRepo
	Publisher      events.Publisher
	BatchSize      int
	Retention      time.Duration
	PublishTimeout time.Duration
	Metrics        *telemetry.Metrics `depcheck:"optional"`
	Logger         *slog.Logger
}

type eventService struct {
	deps EventServiceDeps
}

// NewEventService returns an EventService or an error if a dependency is missing.
func NewEventService(deps EventServiceDeps) (EventService, error) {
	if deps.PublishTimeout == 0 {
		deps.PublishTimeout = 30 * time.Second
	}
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &eventService{deps: deps}, nil
}

func (s *eventService) Publish(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.deps.PublishTimeout)
	defer cancel()
	var total int
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := s.publishBatch(ctx)
		total += n
		if err != nil {
			return total, err
		}
		if n < s.deps.BatchSize {
			return total, nil
		}
	}
}

func (s *eventService) publishBatch(ctx context.Context) (int, error) {
	var n int
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		outbox := s.deps.Outbox.WithTx(tx)
		batch, err := outbox.ClaimOutboxEvents(ctx, s.deps.BatchSize)
		if err != nil {
			return err
		}
		n = len(batch)
		if n == 0 {
			return nil
		}
		evs := make([]events.Event, n)
		ids := make([]int64, n)
		parents := make([]string, 0, n)
		byType := map[string]int{}
		for i, e := range batch {
			evs[i] = events.Event{ID: e.ID, Type: e.Type, OccurredAt: e.OccurredAt, Payload: e.Payload, TraceParent: e.TraceParent}
			ids[i] = e.ID
			parents = append(parents, e.TraceParent)
			byType[e.Type]++
		}
		// The relay's span links to every producing transaction in the batch, so a trace shows the hop to the queue.
		ctx, span := telemetry.StartLinked(ctx, "outbox.publish", parents...)
		defer span.End()
		span.SetAttributes(telemetry.Int64("outbox.batch_size", int64(n)))
		if err := s.deps.Publisher.Publish(ctx, evs); err != nil {
			span.RecordError(err)
			return fmt.Errorf("services: publish events: %w", err)
		}
		for t, c := range byType {
			s.deps.Metrics.Published(ctx, t, c)
		}
		return outbox.MarkOutboxEventsPublished(ctx, ids)
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *eventService) CleanOutbox(ctx context.Context) (int64, error) {
	before := time.Now().Add(-s.deps.Retention)
	var total int64
	for {
		n, err := s.deps.Outbox.DeleteOutboxEventsPublishedBefore(ctx, before, s.deps.BatchSize)
		total += n
		if err != nil {
			return total, err
		}
		if n < int64(s.deps.BatchSize) {
			return total, nil
		}
	}
}

func (s *eventService) ProcessOnce(ctx context.Context, eventID int64, fn func(ctx context.Context) error) (bool, error) {
	if eventID <= 0 || fn == nil {
		return false, errors.New("services: positive event id and handler are required")
	}
	var fresh bool
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		fresh, err = s.deps.Processed.WithTx(tx).MarkEventProcessed(ctx, eventID)
		if err != nil || !fresh {
			return err
		}
		return fn(ctx)
	})
	if err != nil {
		return false, err
	}
	return fresh, nil
}
