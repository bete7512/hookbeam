package ratelimit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/bete7512/scaffold/pkg/ratelimit"
)

func TestWindow(t *testing.T) {
	base := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		now       time.Time
		window    time.Duration
		wantStart time.Time
		wantLeft  time.Duration
	}{
		{name: "at window start", now: base, window: time.Minute, wantStart: base, wantLeft: time.Minute},
		{name: "inside window", now: base.Add(20 * time.Second), window: time.Minute, wantStart: base, wantLeft: 40 * time.Second},
		{name: "last instant before next window", now: base.Add(time.Minute - time.Nanosecond), window: time.Minute, wantStart: base, wantLeft: time.Nanosecond},
		{name: "next window", now: base.Add(time.Minute), window: time.Minute, wantStart: base.Add(time.Minute), wantLeft: time.Minute},
		{name: "non local time is normalised to utc", now: base.Add(20 * time.Second).In(time.FixedZone("plus2", 2*3600)), window: time.Minute, wantStart: base, wantLeft: 40 * time.Second},
		{name: "hour window", now: base.Add(37 * time.Minute), window: time.Hour, wantStart: base, wantLeft: 23 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, left := ratelimit.Window(tt.now, tt.window)
			assert.True(t, tt.wantStart.Equal(start), "start: want %s, got %s", tt.wantStart, start)
			assert.Equal(t, time.UTC, start.Location())
			assert.Equal(t, tt.wantLeft, left)
		})
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name  string
		count int
		limit int
		left  time.Duration
		want  ratelimit.Decision
	}{
		{name: "first hit", count: 1, limit: 5, left: time.Minute, want: ratelimit.Decision{Allowed: true, Remaining: 4, RetryAfter: time.Minute}},
		{name: "count equals limit", count: 5, limit: 5, left: 30 * time.Second, want: ratelimit.Decision{Allowed: true, Remaining: 0, RetryAfter: 30 * time.Second}},
		{name: "count one over limit", count: 6, limit: 5, left: 30 * time.Second, want: ratelimit.Decision{Allowed: false, Remaining: 0, RetryAfter: 30 * time.Second}},
		{name: "far over limit keeps remaining at zero", count: 50, limit: 5, left: time.Second, want: ratelimit.Decision{Allowed: false, Remaining: 0, RetryAfter: time.Second}},
		{name: "zero limit denies", count: 1, limit: 0, left: time.Second, want: ratelimit.Decision{Allowed: false, Remaining: 0, RetryAfter: time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ratelimit.Decide(tt.count, tt.limit, tt.left))
		})
	}
}
