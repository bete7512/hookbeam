package identity

import (
	"context"
	"testing"
	"time"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/cache"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
)

func TestRateLimitStoreTake(t *testing.T) {
	c, err := cache.New(cache.Config{MaxItems: 1000})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	limiter := ratelimitmemory.New(c)
	limiter.Now = func() time.Time { return now }
	store := rateLimitStore{limiter: limiter}
	rule := types.RateLimitRule{Limit: 2, Window: time.Minute, Burst: 1}

	for i := 1; i <= 3; i++ {
		d, err := store.Take(context.Background(), "ip:1", rule, now)
		require.NoError(t, err)
		assert.True(t, d.Allowed, "hit %d", i)
		assert.Equal(t, 3, d.Limit)
		assert.Equal(t, 3-i, d.Remaining)
	}
	d, err := store.Take(context.Background(), "ip:1", rule, now)
	require.NoError(t, err)
	assert.False(t, d.Allowed)
	assert.Equal(t, now.Add(d.RetryAfter), d.ResetAt)

	require.NoError(t, store.Reset(context.Background(), "ip:1"))
	require.NoError(t, store.Close())
}
