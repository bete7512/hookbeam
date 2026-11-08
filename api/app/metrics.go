package app

import (
	"context"

	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// RegisterGauges observes the control plane's outbox on every metrics export; the api process registers it too,
// so a stopped worker still shows as lag rather than as silence.
func RegisterGauges(core *Core) error {
	return telemetry.Gauge("hookbeam.outbox.oldest_age", "age in seconds of the oldest unpublished outbox event", func(ctx context.Context) (float64, error) {
		lag, err := core.Events.OutboxLag(ctx)
		return lag.Seconds(), err
	})
}
