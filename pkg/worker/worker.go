// Package worker runs the periodic loops of a background process.
package worker

import (
	"context"
	"log/slog"
	"time"
)

// Loop describes one periodic job: fn runs once at start, after every Interval and on every Wake signal.
type Loop struct {
	Name     string          // names the job in the warning logged when a run fails
	Interval time.Duration   // time between runs; required
	Timeout  time.Duration   // bound on one run; zero means unbounded
	Wake     <-chan struct{} // optional: a signal runs the job now
	Log      *slog.Logger
}

// Every runs the loop until ctx is done and returns nil; a failed run is logged, never fatal.
func Every(ctx context.Context, l Loop, fn func(ctx context.Context) error) error {
	ticker := time.NewTicker(l.Interval)
	defer ticker.Stop()
	for {
		run(ctx, l, fn)
		select {
		case <-ctx.Done():
			return nil
		case <-l.Wake:
		case <-ticker.C:
		}
	}
}

func run(ctx context.Context, l Loop, fn func(ctx context.Context) error) {
	rctx, cancel := ctx, context.CancelFunc(func() {})
	if l.Timeout > 0 {
		rctx, cancel = context.WithTimeout(ctx, l.Timeout)
	}
	defer cancel()
	if err := fn(rctx); err != nil && ctx.Err() == nil && l.Log != nil {
		l.Log.WarnContext(ctx, l.Name+" failed", "error", err)
	}
}
