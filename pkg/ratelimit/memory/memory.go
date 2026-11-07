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

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// Limiter counts hits per key and window in a cache.Cache; it also serves as goauth's rate-limit store.
type Limiter struct {
	// Now supplies time for tests; configure it before using the limiter.
	Now   func() time.Time
	cache *cache.Cache
}

var _ types.RateLimitStore = (*Limiter)(nil)

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

// Take counts a hit for goauth's rate-limit module; Burst extends the limit.
func (l *Limiter) Take(ctx context.Context, key string, rule types.RateLimitRule, now time.Time) (types.RateLimitDecision, error) {
	limit := rule.Limit + rule.Burst
	d, err := l.Allow(ctx, key, limit, rule.Window)
	if err != nil {
		return types.RateLimitDecision{}, err
	}
	return types.RateLimitDecision{
		Allowed:    d.Allowed,
		Limit:      limit,
		Remaining:  d.Remaining,
		RetryAfter: d.RetryAfter,
		ResetAt:    now.Add(d.RetryAfter),
	}, nil
}

// Reset is a no-op: fixed windows expire on their own and the cache holds nothing addressable.
func (l *Limiter) Reset(context.Context, string) error { return nil }

// Close is a no-op: the cache is owned by whoever built it.
func (l *Limiter) Close() error { return nil }
