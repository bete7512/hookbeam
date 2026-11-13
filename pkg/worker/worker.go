// Package worker runs the recurring jobs of a background process.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/adhocore/gronx"
	"golang.org/x/sync/errgroup"

	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// Job is one recurring piece of work. Exactly one of Every and At sets its rhythm: Every is a cadence, run once
// at start and then after every interval, for drains that must keep up with a queue; At is a cron expression in
// the runner's Location, run at its ticks only, for housekeeping that belongs to the calendar. An exclusive job
// runs on one replica at a time: the replica holding its lock runs it, the others skip until the holder is gone.
type Job struct {
	Name      string
	Every     time.Duration
	At        string
	Timeout   time.Duration   // bound on one run; zero means unbounded
	Wake      <-chan struct{} // optional: a signal runs the job now
	Exclusive bool
	Run       func(ctx context.Context) error
}

// Locks are the per-job locks exclusive jobs hold across runs. Hold takes the lock on name or confirms this
// process still holds it, and reports false while another process has it; a lock ends with its holder.
type Locks interface {
	Hold(ctx context.Context, name string) (bool, error)
}

// Runner runs a process's jobs until the context ends.
type Runner struct {
	Log      *slog.Logger
	Metrics  *telemetry.Metrics // optional; nil records nothing
	Locks    Locks              // required when any job is exclusive
	Location *time.Location     // for At expressions; nil means UTC
	Now      func() time.Time   // optional clock, for tests
}

// Results a run can end with; the metric label values.
const (
	ResultOK      = "ok"
	ResultError   = "error"
	ResultSkipped = "skipped" // another replica holds the job
)

// Run validates the jobs, then runs each in its own goroutine until ctx ends. A failed run is logged and counted,
// never fatal.
func (r Runner) Run(ctx context.Context, jobs []Job) error {
	if err := r.validate(jobs); err != nil {
		return err
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, j := range jobs {
		g.Go(func() error {
			r.loop(gctx, j)
			return nil
		})
	}
	return g.Wait()
}

func (r Runner) validate(jobs []Job) error {
	seen := make(map[string]bool, len(jobs))
	for _, j := range jobs {
		switch {
		case j.Name == "":
			return errors.New("worker: a job needs a name")
		case seen[j.Name]:
			return fmt.Errorf("worker: job %q is declared twice", j.Name)
		case j.Run == nil:
			return fmt.Errorf("worker: job %q needs a Run function", j.Name)
		case (j.Every > 0) == (j.At != ""):
			return fmt.Errorf("worker: job %q needs exactly one of Every and At", j.Name)
		case j.Every < 0:
			return fmt.Errorf("worker: job %q has a negative Every", j.Name)
		case j.At != "" && !gronx.IsValid(j.At):
			return fmt.Errorf("worker: job %q has an invalid cron expression %q", j.Name, j.At)
		case j.Exclusive && r.Locks == nil:
			return fmt.Errorf("worker: job %q is exclusive but the runner has no Locks", j.Name)
		}
		seen[j.Name] = true
	}
	return nil
}

func (r Runner) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Runner) location() *time.Location {
	if r.Location != nil {
		return r.Location
	}
	return time.UTC
}

// loop drives one job: a cadence job runs now, a calendar job waits for its first tick; then both run on every
// wake-up and every tick. held remembers whether this replica holds an exclusive job, so hand-overs are logged.
func (r Runner) loop(ctx context.Context, j Job) {
	held := false
	if j.At == "" {
		held = r.attempt(ctx, j, held)
	}
	for {
		timer := time.NewTimer(r.next(j).Sub(r.now()))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-j.Wake:
			timer.Stop()
		case <-timer.C:
		}
		held = r.attempt(ctx, j, held)
	}
}

// next is the moment of the following tick.
func (r Runner) next(j Job) time.Time {
	now := r.now()
	if j.At == "" {
		return now.Add(j.Every)
	}
	next, err := gronx.NextTickAfter(j.At, now.In(r.location()), false)
	if err != nil {
		return now.Add(time.Minute) // validated at start; unreachable
	}
	return next
}

// attempt runs the job once, behind its lock when exclusive, and reports whether this replica holds the lock.
func (r Runner) attempt(ctx context.Context, j Job, held bool) bool {
	if ctx.Err() != nil {
		return held
	}
	if j.Exclusive {
		ok, err := r.Locks.Hold(ctx, j.Name)
		if err != nil {
			r.finish(ctx, j, 0, fmt.Errorf("hold lock: %w", err))
			return false
		}
		if ok && !held && r.Log != nil {
			r.Log.InfoContext(ctx, j.Name+": this replica runs it", "job", j.Name)
		}
		if !ok {
			if held && r.Log != nil {
				r.Log.WarnContext(ctx, j.Name+": lock lost to another replica", "job", j.Name)
			}
			r.Metrics.JobRun(ctx, j.Name, ResultSkipped, 0)
			return false
		}
	}
	rctx, cancel := ctx, context.CancelFunc(func() {})
	if j.Timeout > 0 {
		rctx, cancel = context.WithTimeout(ctx, j.Timeout)
	}
	start := r.now()
	err := j.Run(rctx)
	cancel()
	r.finish(ctx, j, r.now().Sub(start), err)
	return j.Exclusive
}

func (r Runner) finish(ctx context.Context, j Job, elapsed time.Duration, err error) {
	if err == nil {
		r.Metrics.JobRun(ctx, j.Name, ResultOK, elapsed)
		return
	}
	r.Metrics.JobRun(ctx, j.Name, ResultError, elapsed)
	if ctx.Err() == nil && r.Log != nil {
		r.Log.WarnContext(ctx, j.Name+" failed", "error", err)
	}
}
