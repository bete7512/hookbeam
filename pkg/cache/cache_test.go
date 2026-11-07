package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/cache"
)

func newCache(t *testing.T) *cache.Cache {
	t.Helper()
	c, err := cache.New(cache.Config{MaxItems: 100})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     cache.Config
		wantErr bool
	}{
		{name: "valid", cfg: cache.Config{MaxItems: 10}},
		{name: "zero size", cfg: cache.Config{}, wantErr: true},
		{name: "negative counter capacity", cfg: cache.Config{MaxItems: 10, MaxCounters: -1}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := cache.New(tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			c.Close()
		})
	}
}

func TestSetGet(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  any
		ttl    time.Duration
		sleep  time.Duration
		lookup string
		want   any
		found  bool
	}{
		{name: "stored value is returned", key: "a", value: 42, ttl: time.Minute, lookup: "a", want: 42, found: true},
		{name: "other key misses", key: "a", value: 42, ttl: time.Minute, lookup: "b"},
		{name: "expired value misses", key: "a", value: 42, ttl: 10 * time.Millisecond, sleep: 50 * time.Millisecond, lookup: "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCache(t)
			require.True(t, c.Set(tt.key, tt.value, tt.ttl))
			c.Wait()
			time.Sleep(tt.sleep)

			got, ok := c.Get(tt.lookup)

			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestClose(t *testing.T) {
	c, err := cache.New(cache.Config{MaxItems: 10})
	require.NoError(t, err)
	c.Set("a", 1, time.Minute)
	c.Wait()

	c.Close()

	_, ok := c.Get("a")
	require.False(t, ok)
	c.Wait()
}

func TestIncrementWindow(t *testing.T) {
	c, err := cache.New(cache.Config{MaxItems: 100, MaxCounters: 2})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	ctx := context.Background()
	count, left, err := c.IncrementWindow(ctx, "long", time.Hour, clock)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, time.Hour, left)
	_, _, err = c.IncrementWindow(ctx, "short", time.Minute, clock)
	require.NoError(t, err)
	_, _, err = c.IncrementWindow(ctx, "third", time.Minute, clock)
	require.ErrorIs(t, err, cache.ErrCounterCapacity)
	now = now.Add(time.Minute)
	_, _, err = c.IncrementWindow(ctx, "third", time.Minute, clock)
	require.NoError(t, err, "shorter window expires before the earlier insertion")
	count, left, err = c.IncrementWindow(ctx, "long", time.Hour, clock)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, 59*time.Minute, left)
	_, _, err = c.IncrementWindow(ctx, "invalid", 0, clock)
	require.Error(t, err)
}
