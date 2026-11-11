package identity

import (
	"context"
	"time"

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// rateLimitStore counts goauth's rate limits in Hookbeam's limiter; Burst extends the rule's limit.
type rateLimitStore struct {
	limiter ratelimit.Limiter
}

var _ types.RateLimitStore = rateLimitStore{}

func (s rateLimitStore) Take(ctx context.Context, key string, rule types.RateLimitRule, now time.Time) (types.RateLimitDecision, error) {
	limit := rule.Limit + rule.Burst
	d, err := s.limiter.Allow(ctx, key, limit, rule.Window)
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
func (rateLimitStore) Reset(context.Context, string) error { return nil }

// Close is a no-op: the limiter is owned by whoever built it.
func (rateLimitStore) Close() error { return nil }
