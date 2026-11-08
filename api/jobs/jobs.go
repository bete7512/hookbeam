// Package jobs dispatches worker events with replay suppression and bounded processing.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// Handler processes one event.
type Handler func(ctx context.Context, e events.Event) error

// Deps are the dependencies; HandlerTimeout defaults to two minutes.
type Deps struct {
	Events         services.EventService
	Logger         *slog.Logger
	HandlerTimeout time.Duration
	// Handlers maps event types to handlers; the delivery domain registers its own.
	Handlers map[string]Handler
	Metrics  *telemetry.Metrics // optional
}

// Jobs dispatches events to the registered handlers.
type Jobs struct {
	deps     Deps
	handlers map[string]Handler
}

// New returns the jobs or an error if a dependency is missing.
func New(deps Deps) (*Jobs, error) {
	if deps.HandlerTimeout == 0 {
		deps.HandlerTimeout = 2 * time.Minute
	}
	if deps.Events == nil || deps.Logger == nil || deps.HandlerTimeout <= 0 {
		return nil, errors.New("jobs: Events, Logger and positive HandlerTimeout are required")
	}
	handlers := make(map[string]Handler, len(deps.Handlers))
	for t, h := range deps.Handlers {
		if t == "" || h == nil {
			return nil, errors.New("jobs: handlers need a type and a function")
		}
		handlers[t] = h
	}
	return &Jobs{deps: deps, handlers: handlers}, nil
}

// Handle rejects unsupported events for redelivery and DLQ routing, and suppresses recorded successful replays.
func (j *Jobs) Handle(ctx context.Context, e events.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	h, ok := j.handlers[e.Type]
	if !ok {
		j.deps.Logger.WarnContext(ctx, "no handler for event", "type", e.Type, "event_id", e.ID)
		return fmt.Errorf("jobs: no handler for event type %q", e.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, j.deps.HandlerTimeout)
	defer cancel()
	// A consumer span linked to the transaction that produced the event; the queue broke the parent chain.
	ctx, span := telemetry.StartLinked(ctx, "event "+e.Type, e.TraceParent)
	defer span.End()
	span.SetAttributes(telemetry.String("hookbeam.event.type", e.Type), telemetry.Int64("hookbeam.event.id", e.ID))
	fresh, err := j.deps.Events.ProcessOnce(ctx, e.ID, func(ctx context.Context) error {
		return h(ctx, e)
	})
	if err := errors.Join(err, ctx.Err()); err != nil {
		span.RecordError(err)
		j.deps.Metrics.Handled(ctx, e.Type, "error")
		return err
	}
	if !fresh {
		j.deps.Metrics.Handled(ctx, e.Type, "replay")
		j.deps.Logger.InfoContext(ctx, "event already processed", "type", e.Type, "event_id", e.ID)
		return nil
	}
	j.deps.Metrics.Handled(ctx, e.Type, "ok")
	return nil
}
