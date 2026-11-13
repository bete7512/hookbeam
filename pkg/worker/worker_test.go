package worker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/worker"
)

// fakeLocks answers Hold from a flag and counts the calls.
type fakeLocks struct {
	mu     sync.Mutex
	refuse bool
	err    error
	calls  []string
}

func (f *fakeLocks) Hold(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
	return !f.refuse, f.err
}

func (f *fakeLocks) set(refuse bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuse = refuse
}

func (f *fakeLocks) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// logBuffer is a bytes.Buffer safe to read while the runner logs into it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// start runs the jobs in the background and returns the cancel and a channel with the result.
func start(r worker.Runner, jobs []worker.Job) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, jobs) }()
	return cancel, done
}

func TestRunnerValidate(t *testing.T) {
	ok := func(context.Context) error { return nil }
	tests := []struct {
		name   string
		runner worker.Runner
		jobs   []worker.Job
		want   string
	}{
		{name: "no name", jobs: []worker.Job{{Every: time.Second, Run: ok}}, want: "needs a name"},
		{name: "twice", jobs: []worker.Job{{Name: "a", Every: time.Second, Run: ok}, {Name: "a", Every: time.Second, Run: ok}}, want: "declared twice"},
		{name: "no run", jobs: []worker.Job{{Name: "a", Every: time.Second}}, want: "needs a Run"},
		{name: "neither rhythm", jobs: []worker.Job{{Name: "a", Run: ok}}, want: "exactly one of Every and At"},
		{name: "both rhythms", jobs: []worker.Job{{Name: "a", Every: time.Second, At: "* * * * *", Run: ok}}, want: "exactly one of Every and At"},
		{name: "bad cron", jobs: []worker.Job{{Name: "a", At: "every day", Run: ok}}, want: "invalid cron expression"},
		{name: "exclusive without locks", jobs: []worker.Job{{Name: "a", Every: time.Second, Exclusive: true, Run: ok}}, want: "no Locks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := tt.runner.Run(ctx, tt.jobs)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestRunnerEvery(t *testing.T) {
	t.Run("runs at start, on wake and on the tick, and stops with the context", func(t *testing.T) {
		var runs atomic.Int32
		wake := make(chan struct{}, 1)
		cancel, done := start(worker.Runner{}, []worker.Job{{
			Name:  "job",
			Every: 20 * time.Millisecond,
			Wake:  wake,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		require.Eventually(t, func() bool { return runs.Load() >= 1 }, time.Second, time.Millisecond, "the first run is immediate")
		wake <- struct{}{}
		require.Eventually(t, func() bool { return runs.Load() >= 2 }, time.Second, time.Millisecond, "a wake-up runs the job")
		require.Eventually(t, func() bool { return runs.Load() >= 3 }, time.Second, time.Millisecond, "the tick runs the job")
		cancel()
		require.NoError(t, <-done)
	})

	t.Run("a failed run is logged and the loop goes on", func(t *testing.T) {
		var buf logBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		var runs atomic.Int32
		cancel, done := start(worker.Runner{Log: log}, []worker.Job{{
			Name:  "prune audit",
			Every: 5 * time.Millisecond,
			Run: func(context.Context) error {
				runs.Add(1)
				return errors.New("boom")
			},
		}})
		require.Eventually(t, func() bool { return runs.Load() >= 2 }, time.Second, time.Millisecond)
		cancel()
		require.NoError(t, <-done)
		assert.Contains(t, buf.String(), `msg="prune audit failed"`)
		assert.Contains(t, buf.String(), "error=boom")
	})

	t.Run("a timeout bounds one run", func(t *testing.T) {
		var deadline atomic.Bool
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- worker.Runner{}.Run(ctx, []worker.Job{{
				Name:    "job",
				Every:   time.Hour,
				Timeout: time.Millisecond,
				Run: func(rctx context.Context) error {
					_, ok := rctx.Deadline()
					deadline.Store(ok)
					cancel()
					return nil
				},
			}})
		}()
		require.NoError(t, <-done)
		assert.True(t, deadline.Load())
	})

	t.Run("two jobs run independently", func(t *testing.T) {
		var a, b atomic.Int32
		cancel, done := start(worker.Runner{}, []worker.Job{
			{Name: "a", Every: 5 * time.Millisecond, Run: func(context.Context) error { a.Add(1); return nil }},
			{Name: "b", Every: 5 * time.Millisecond, Run: func(context.Context) error {
				b.Add(1)
				time.Sleep(50 * time.Millisecond)
				return nil
			}},
		})
		require.Eventually(t, func() bool { return a.Load() >= 5 && b.Load() >= 1 }, time.Second, time.Millisecond, "a slow job never holds up a fast one")
		cancel()
		require.NoError(t, <-done)
	})
}

func TestRunnerAt(t *testing.T) {
	t.Run("a calendar job waits for its tick instead of running at start", func(t *testing.T) {
		var runs atomic.Int32
		wake := make(chan struct{}, 1)
		cancel, done := start(worker.Runner{}, []worker.Job{{
			Name: "prune audit",
			At:   "0 3 * * *",
			Wake: wake,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		time.Sleep(30 * time.Millisecond)
		assert.Zero(t, runs.Load(), "no run before the first tick")
		wake <- struct{}{}
		require.Eventually(t, func() bool { return runs.Load() == 1 }, time.Second, time.Millisecond, "a wake-up still runs it")
		cancel()
		require.NoError(t, <-done)
	})

	t.Run("the first tick is computed in the runner's location", func(t *testing.T) {
		brussels, err := time.LoadLocation("Europe/Brussels")
		require.NoError(t, err)
		now := time.Date(2026, 11, 13, 1, 30, 0, 0, time.UTC) // 02:30 in Brussels: the 03:00 tick is thirty minutes away
		var runs atomic.Int32
		cancel, done := start(worker.Runner{Location: brussels, Now: func() time.Time { return now }}, []worker.Job{{
			Name: "prune audit",
			At:   "0 3 * * *",
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		time.Sleep(30 * time.Millisecond)
		assert.Zero(t, runs.Load(), "the tick is in the future, so nothing runs")
		cancel()
		require.NoError(t, <-done)
	})
}

func TestRunnerExclusive(t *testing.T) {
	t.Run("holds the lock before every run and runs while it is held", func(t *testing.T) {
		locks := &fakeLocks{}
		var runs atomic.Int32
		cancel, done := start(worker.Runner{Locks: locks}, []worker.Job{{
			Name:      "clean outbox",
			Every:     5 * time.Millisecond,
			Exclusive: true,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		require.Eventually(t, func() bool { return runs.Load() >= 3 }, time.Second, time.Millisecond)
		cancel()
		require.NoError(t, <-done)
		assert.GreaterOrEqual(t, locks.count(), int(runs.Load()), "one Hold per run")
		assert.Equal(t, "clean outbox", locks.calls[0])
	})

	t.Run("skips while another replica holds the job, and takes over when it is free", func(t *testing.T) {
		var buf logBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		locks := &fakeLocks{refuse: true}
		var runs atomic.Int32
		cancel, done := start(worker.Runner{Locks: locks, Log: log}, []worker.Job{{
			Name:      "clean outbox",
			Every:     5 * time.Millisecond,
			Exclusive: true,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		require.Eventually(t, func() bool { return locks.count() >= 3 }, time.Second, time.Millisecond)
		assert.Zero(t, runs.Load(), "nothing runs without the lock")
		locks.set(false)
		require.Eventually(t, func() bool { return runs.Load() >= 1 }, time.Second, time.Millisecond, "the job runs once the lock is free")
		locks.set(true)
		require.Eventually(t, func() bool { return strings.Contains(buf.String(), "lock lost to another replica") }, time.Second, time.Millisecond)
		cancel()
		require.NoError(t, <-done)
		assert.Contains(t, buf.String(), "this replica runs it")
	})

	t.Run("a lock error is a failed run, not a crash", func(t *testing.T) {
		var buf logBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		locks := &fakeLocks{err: errors.New("connection reset")}
		var runs atomic.Int32
		cancel, done := start(worker.Runner{Locks: locks, Log: log}, []worker.Job{{
			Name:      "clean outbox",
			Every:     5 * time.Millisecond,
			Exclusive: true,
			Run: func(context.Context) error {
				runs.Add(1)
				return nil
			},
		}})
		require.Eventually(t, func() bool { return locks.count() >= 2 }, time.Second, time.Millisecond)
		cancel()
		require.NoError(t, <-done)
		assert.Zero(t, runs.Load())
		assert.Contains(t, buf.String(), `msg="clean outbox failed"`)
		assert.Contains(t, buf.String(), "hold lock: connection reset")
	})
}
