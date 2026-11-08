package app

import (
	"context"

	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// RegisterGauges observes the queue's shape on every metrics export. Both the server and the worker register
// them, so the numbers keep arriving when the worker is the thing that died.
func RegisterGauges(core *Core) error {
	if err := telemetry.Gauge("hookbeam.outbox.oldest_age", "age in seconds of the oldest unpublished outbox event", func(ctx context.Context) (float64, error) {
		lag, err := core.Events.OutboxLag(ctx)
		return lag.Seconds(), err
	}); err != nil {
		return err
	}
	if err := telemetry.Gauge("hookbeam.deliveries.oldest_due_age", "age in seconds of the oldest delivery that is due and not yet claimed", func(ctx context.Context) (float64, error) {
		b, err := core.Dispatch.Backlog(ctx)
		return b.OldestDueAge.Seconds(), err
	}); err != nil {
		return err
	}
	return telemetry.Gauge("hookbeam.deliveries.in_flight", "deliveries under a lease right now", func(ctx context.Context) (float64, error) {
		b, err := core.Dispatch.Backlog(ctx)
		return float64(b.InFlight), err
	})
}
