// Package memory counts requests in shared, bounded in-process counters.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// Limiter counts hits per key and window in a cache.Cache.
type Limiter struct {
	// Now supplies time for tests; configure it before using the limiter.
	Now   func() time.Time
	cache *cache.Cache
}

// New returns a Limiter that shares c's atomic counter storage.
func New(c *cache.Cache) *Limiter {
	return &Limiter{cache: c}
}

// Allow counts a hit, failing closed when live counter capacity is exhausted.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (ratelimit.Decision, error) {
	if limit <= 0 || limit == math.MaxInt || window <= 0 {
		return ratelimit.Decision{}, errors.New("ratelimit: positive bounded limit and window required")
	}
	if l.cache == nil {
		return ratelimit.Decision{}, ratelimit.ErrUnavailable
	}
	digest := sha256.Sum256([]byte(key))
	count, left, err := l.cache.IncrementWindow(ctx, "ratelimit:"+hex.EncodeToString(digest[:]), window, l.Now)
	if errors.Is(err, cache.ErrCounterCapacity) || errors.Is(err, cache.ErrClosed) {
		return ratelimit.Decision{}, fmt.Errorf("%w: %w", ratelimit.ErrUnavailable, err)
	}
	if err != nil {
		return ratelimit.Decision{}, err
	}
	return ratelimit.Decide(count, limit, left), nil
}
