// Package ratelimit decides whether a key may act again inside a window.
package ratelimit

import (
	"context"
	"errors"
	"time"
)

// ErrUnavailable means the limiter cannot safely account for a request.
var ErrUnavailable = errors.New("rate limiter unavailable")

// Decision is the outcome of one Allow call.
type Decision struct {
	Allowed    bool
	Remaining  int           // attempts left in the window after this one (0 when denied)
	RetryAfter time.Duration // time until the window resets; set whether or not the call was allowed
}

// Limiter counts hits per key in fixed windows. Allow records the hit and reports whether it stayed within limit.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Decision, error)
}

// Window returns the start of the fixed window containing now, and the time left in it.
func Window(now time.Time, window time.Duration) (start time.Time, left time.Duration) {
	start = now.UTC().Truncate(window)
	return start, start.Add(window).Sub(now)
}

// Decide builds the Decision for the count-th hit in a window with left remaining.
func Decide(count, limit int, left time.Duration) Decision {
	return Decision{
		Allowed:    count <= limit,
		Remaining:  max(limit-count, 0),
		RetryAfter: left,
	}
}
