package worker_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/worker"
)

func TestEvery(t *testing.T) {
	t.Run("runs at start, on wake and on the tick, and stops with the context", func(t *testing.T) {
		var runs atomic.Int32
		wake := make(chan struct{}, 1)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- worker.Every(ctx, worker.Loop{Name: "job", Interval: 20 * time.Millisecond, Wake: wake}, func(context.Context) error {
				runs.Add(1)
				return nil
			})
		}()
		require.Eventually(t, func() bool { return runs.Load() >= 1 }, time.Second, time.Millisecond, "the first run is immediate")
		wake <- struct{}{}
		require.Eventually(t, func() bool { return runs.Load() >= 2 }, time.Second, time.Millisecond, "a wake-up runs the job")
		require.Eventually(t, func() bool { return runs.Load() >= 3 }, time.Second, time.Millisecond, "the tick runs the job")
		cancel()
		require.NoError(t, <-done)
	})

	t.Run("a failed run is logged and the loop goes on", func(t *testing.T) {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		var runs atomic.Int32
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			done <- worker.Every(ctx, worker.Loop{Name: "prune audit", Interval: 5 * time.Millisecond, Log: log}, func(context.Context) error {
				runs.Add(1)
				return errors.New("boom")
			})
		}()
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
			done <- worker.Every(ctx, worker.Loop{Name: "job", Interval: time.Hour, Timeout: time.Millisecond}, func(rctx context.Context) error {
				_, ok := rctx.Deadline()
				deadline.Store(ok)
				cancel()
				return nil
			})
		}()
		require.NoError(t, <-done)
		assert.True(t, deadline.Load())
	})
}
