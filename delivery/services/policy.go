package services

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/bete7512/hookbeam/delivery/models"
)

// Failure reasons recorded on deliveries.
const (
	ReasonAttemptsExhausted = "attempts_exhausted"
	ReasonDeadlineExceeded  = "deadline_exceeded"
	ReasonEndpointArchived  = "endpoint_archived"
	ReasonCancelled         = "cancelled" // written by the repo when a cancel request settles
)

// Policy is the retry schedule: Delays[i] follows attempt i+1; MaxAttempts caps the cycle; CycleMaxAge bounds it in time.
type Policy struct {
	Delays      []time.Duration
	MaxAttempts int32
	CycleMaxAge time.Duration
	Jitter      float64 // fraction of the delay added or removed at random
}

// DefaultPolicy is the product specification's: 15s, 1m, 5m, 30m, 2h, 6h, 12h; eight attempts; 24 hours.
var DefaultPolicy = Policy{
	Delays:      []time.Duration{15 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 12 * time.Hour},
	MaxAttempts: 8,
	CycleMaxAge: 24 * time.Hour,
	Jitter:      0.2,
}

// Decision is the delivery's next state after an attempt.
type Decision struct {
	State  string
	DueAt  *time.Time
	Reason *string
}

// Retryable reports whether a result deserves another attempt: no response, 408, 425, 429 or 5xx.
// A blocked destination never is: the URL will not become allowed by waiting.
func Retryable(res SendResult) bool {
	if res.StatusCode == 0 {
		return res.ErrorClass != "blocked_destination"
	}
	return res.StatusCode == http.StatusRequestTimeout || res.StatusCode == http.StatusTooEarly ||
		res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500
}

// Decide maps the result of attempt number attempt (1-based) to the delivery's next state.
func (p Policy) Decide(res SendResult, attempt int32, deadline, now time.Time) Decision {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return Decision{State: models.DeliverySucceeded}
	}
	reason := failureReason(res)
	if !Retryable(res) {
		return Decision{State: models.DeliveryFailed, Reason: &reason}
	}
	if attempt >= p.MaxAttempts || int(attempt) > len(p.Delays) {
		exhausted := ReasonAttemptsExhausted
		return Decision{State: models.DeliveryFailed, Reason: &exhausted}
	}
	delay := p.Delays[attempt-1]
	if p.Jitter > 0 {
		delay += time.Duration((rand.Float64()*2 - 1) * p.Jitter * float64(delay)) //nolint:gosec // jitter, not security
	}
	next := now.Add(delay)
	if wait := RetryAfter(res.Headers["retry-after"], now); wait > 0 && now.Add(wait).After(next) {
		next = now.Add(wait)
	}
	if next.After(deadline) {
		expired := ReasonDeadlineExceeded
		return Decision{State: models.DeliveryFailed, Reason: &expired}
	}
	return Decision{State: models.DeliveryRetryScheduled, DueAt: &next, Reason: &reason}
}

// RetryAfter parses a Retry-After value (seconds or HTTP date) into a wait from now; zero when absent or invalid.
func RetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

func failureReason(res SendResult) string {
	if res.StatusCode == 0 {
		return res.ErrorClass
	}
	return fmt.Sprintf("http_%d", res.StatusCode)
}
