package memory_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
	"github.com/bete7512/hookbeam/pkg/ratelimit/memory"
)

// clock is a settable time source for a Limiter.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.New(cache.Config{MaxItems: 1000})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func newLimiter(t *testing.T) (*memory.Limiter, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	l := memory.New(newCache(t))
	l.Now = c.Now
	return l, c
}

func TestLimiterAllow(t *testing.T) {
	ctx := context.Background()
	const limit, window = 3, time.Minute

	tests := []struct {
		name string
		run  func(t *testing.T, l *memory.Limiter, c *clock)
	}{
		{
			name: "allows limit hits then denies with retry after",
			run: func(t *testing.T, l *memory.Limiter, _ *clock) {
				for i := 1; i <= limit; i++ {
					d, err := l.Allow(ctx, "k", limit, window)
					require.NoError(t, err)
					assert.True(t, d.Allowed, "hit %d", i)
					assert.Equal(t, limit-i, d.Remaining, "hit %d", i)
					assert.Equal(t, window, d.RetryAfter)
				}
				d, err := l.Allow(ctx, "k", limit, window)
				require.NoError(t, err)
				assert.False(t, d.Allowed)
				assert.Equal(t, 0, d.Remaining)
				assert.Positive(t, d.RetryAfter)
			},
		},
		{
			name: "retry after shrinks as the window passes",
			run: func(t *testing.T, l *memory.Limiter, c *clock) {
				c.Advance(45 * time.Second)
				d, err := l.Allow(ctx, "k", limit, window)
				require.NoError(t, err)
				assert.Equal(t, 15*time.Second, d.RetryAfter)
			},
		},
		{
			name: "a new window resets the count",
			run: func(t *testing.T, l *memory.Limiter, c *clock) {
				for range limit + 1 {
					_, err := l.Allow(ctx, "k", limit, window)
					require.NoError(t, err)
				}
				c.Advance(window)
				d, err := l.Allow(ctx, "k", limit, window)
				require.NoError(t, err)
				assert.True(t, d.Allowed)
				assert.Equal(t, limit-1, d.Remaining)
			},
		},
		{
			name: "keys are independent",
			run: func(t *testing.T, l *memory.Limiter, _ *clock) {
				for range limit + 1 {
					_, err := l.Allow(ctx, "a", limit, window)
					require.NoError(t, err)
				}
				d, err := l.Allow(ctx, "b", limit, window)
				require.NoError(t, err)
				assert.True(t, d.Allowed)
				assert.Equal(t, limit-1, d.Remaining)
			},
		},
		{
			name: "changing the window for a key starts a fresh count",
			run: func(t *testing.T, l *memory.Limiter, _ *clock) {
				for range limit {
					_, err := l.Allow(ctx, "k", limit, window)
					require.NoError(t, err)
				}
				d, err := l.Allow(ctx, "k", limit, time.Hour)
				require.NoError(t, err)
				assert.True(t, d.Allowed)
				assert.Equal(t, limit-1, d.Remaining)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, c := newLimiter(t)
			tt.run(t, l, c)
		})
	}
}

func TestLimiterAllowConcurrent(t *testing.T) {
	const goroutines, perGoroutine, limit = 20, 10, 50
	l, _ := newLimiter(t)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perGoroutine {
				d, err := l.Allow(context.Background(), "k", limit, time.Minute)
				assert.NoError(t, err)
				if d.Allowed {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, limit, allowed)
}

func TestLimitersShareTheCache(t *testing.T) {
	shared := newCache(t)
	a, b := memory.New(shared), memory.New(shared)

	for range 3 {
		_, err := a.Allow(context.Background(), "k", 3, time.Minute)
		require.NoError(t, err)
	}
	d, err := b.Allow(context.Background(), "k", 3, time.Minute)

	require.NoError(t, err)
	assert.False(t, d.Allowed, "a second limiter over the same cache sees the first one's hits")
}

func TestLimiterAllowCapacity(t *testing.T) {
	store, err := cache.New(cache.Config{MaxItems: 4})
	require.NoError(t, err)
	t.Cleanup(store.Close)
	l := memory.New(store)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	l.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := range 4 {
		d, allowErr := l.Allow(ctx, fmt.Sprint(i), 1, time.Minute)
		require.NoError(t, allowErr)
		require.True(t, d.Allowed)
	}
	for i := range 100 {
		store.Set(fmt.Sprintf("value:%d", i), i, time.Hour)
		d, allowErr := l.Allow(ctx, fmt.Sprintf("new:%d", i), 1, time.Minute)
		require.ErrorIs(t, allowErr, ratelimit.ErrUnavailable)
		require.ErrorIs(t, allowErr, cache.ErrCounterCapacity)
		require.False(t, d.Allowed)
	}
	store.Wait()
	for i := range 4 {
		d, allowErr := l.Allow(ctx, fmt.Sprint(i), 1, time.Minute)
		require.NoError(t, allowErr)
		require.False(t, d.Allowed, "capacity pressure must retain live counters")
	}
	now = now.Add(time.Minute)
	d, err := l.Allow(ctx, "new", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, d.Allowed, "expired counters release capacity")
}

func TestLimitersShareTheCacheConcurrent(t *testing.T) {
	shared := newCache(t)
	limiters := []*memory.Limiter{memory.New(shared), memory.New(shared)}
	for _, l := range limiters {
		l.Now = func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }
	}
	var allowed atomic.Int64
	var wg sync.WaitGroup
	errors := make(chan error, 200)
	for i := range 200 {
		wg.Go(func() {
			d, err := limiters[i%2].Allow(context.Background(), "shared", 50, time.Minute)
			if err != nil {
				errors <- err
				return
			}
			if d.Allowed {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int64(50), allowed.Load())
}

func TestLimiterAllowWindowBoundary(t *testing.T) {
	shared := newCache(t)
	a, b := memory.New(shared), memory.New(shared)
	old := time.Date(2026, 9, 16, 12, 0, 59, 0, time.UTC)
	entered, release := make(chan struct{}), make(chan struct{})
	sampled := make(chan struct{}, 1)
	a.Now = func() time.Time {
		close(entered)
		<-release
		return old
	}
	b.Now = func() time.Time {
		sampled <- struct{}{}
		return old.Add(time.Second)
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := a.Allow(context.Background(), "shared", 2, time.Minute)
		firstDone <- err
	}()
	<-entered
	go func() {
		_, err := b.Allow(context.Background(), "shared", 2, time.Minute)
		secondDone <- err
	}()
	sampledEarly := false
	select {
	case <-sampled:
		sampledEarly = true
		t.Error("another limiter sampled time before the current counter mutation completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)
	if !sampledEarly {
		<-sampled
	}
	b.Now = func() time.Time { return old.Add(time.Second) }
	d, err := b.Allow(context.Background(), "shared", 2, time.Minute)
	require.NoError(t, err)
	require.True(t, d.Allowed)
	d, err = b.Allow(context.Background(), "shared", 2, time.Minute)
	require.NoError(t, err)
	require.False(t, d.Allowed, "the newer window retains its existing hit")
}

func TestLimiterAllowCanceledAndClosed(t *testing.T) {
	store := newCache(t)
	l := memory.New(store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err := l.Allow(ctx, "k", 1, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, d.Allowed)
	d, err = l.Allow(context.Background(), "k", 1, time.Minute)
	require.NoError(t, err)
	require.True(t, d.Allowed, "canceled requests must not consume the allowance")
	store.Close()
	d, err = l.Allow(context.Background(), "other", 1, time.Minute)
	require.ErrorIs(t, err, ratelimit.ErrUnavailable)
	require.ErrorIs(t, err, cache.ErrClosed)
	require.False(t, d.Allowed)
}

var _ ratelimit.Limiter = (*memory.Limiter)(nil)
