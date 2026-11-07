package services_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/services"
)

func TestRetryable(t *testing.T) {
	for _, code := range []int{408, 425, 429, 500, 502, 503, 599} {
		require.True(t, services.Retryable(services.SendResult{StatusCode: code}), code)
	}
	for _, code := range []int{200, 204, 301, 302, 400, 401, 404, 410, 422} {
		require.False(t, services.Retryable(services.SendResult{StatusCode: code}), code)
	}
	require.True(t, services.Retryable(services.SendResult{ErrorClass: "timeout"}))
	require.True(t, services.Retryable(services.SendResult{ErrorClass: "connection"}))
	require.False(t, services.Retryable(services.SendResult{ErrorClass: "blocked_destination"}))
}

func TestPolicyDecide(t *testing.T) {
	policy := services.DefaultPolicy
	policy.Jitter = 0
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	deadline := now.Add(24 * time.Hour)
	tests := []struct {
		name       string
		res        services.SendResult
		attempt    int32
		deadline   time.Time
		wantState  string
		wantDue    time.Duration
		wantReason string
	}{
		{name: "2xx succeeds", res: services.SendResult{StatusCode: 204}, attempt: 1, deadline: deadline, wantState: models.DeliverySucceeded},
		{name: "4xx fails", res: services.SendResult{StatusCode: 404}, attempt: 1, deadline: deadline, wantState: models.DeliveryFailed, wantReason: "http_404"},
		{name: "3xx fails", res: services.SendResult{StatusCode: 302}, attempt: 1, deadline: deadline, wantState: models.DeliveryFailed, wantReason: "http_302"},
		{name: "blocked destination fails", res: services.SendResult{ErrorClass: "blocked_destination"}, attempt: 1, deadline: deadline, wantState: models.DeliveryFailed, wantReason: "blocked_destination"},
		{name: "first 503 retries in 15s", res: services.SendResult{StatusCode: 503}, attempt: 1, deadline: deadline, wantState: models.DeliveryRetryScheduled, wantDue: 15 * time.Second, wantReason: "http_503"},
		{name: "timeout on attempt 3 retries in 5m", res: services.SendResult{ErrorClass: "timeout"}, attempt: 3, deadline: deadline, wantState: models.DeliveryRetryScheduled, wantDue: 5 * time.Minute, wantReason: "timeout"},
		{name: "seventh failure retries in 12h", res: services.SendResult{StatusCode: 500}, attempt: 7, deadline: deadline, wantState: models.DeliveryRetryScheduled, wantDue: 12 * time.Hour, wantReason: "http_500"},
		{name: "eighth failure exhausts", res: services.SendResult{StatusCode: 500}, attempt: 8, deadline: deadline, wantState: models.DeliveryFailed, wantReason: services.ReasonAttemptsExhausted},
		{name: "retry-after later than backoff wins", res: services.SendResult{StatusCode: 429, Headers: map[string]string{"retry-after": "120"}}, attempt: 1, deadline: deadline, wantState: models.DeliveryRetryScheduled, wantDue: 2 * time.Minute, wantReason: "http_429"},
		{name: "retry-after earlier than backoff is ignored", res: services.SendResult{StatusCode: 429, Headers: map[string]string{"retry-after": "1"}}, attempt: 2, deadline: deadline, wantState: models.DeliveryRetryScheduled, wantDue: time.Minute, wantReason: "http_429"},
		{name: "next attempt past the deadline expires", res: services.SendResult{StatusCode: 503}, attempt: 1, deadline: now.Add(10 * time.Second), wantState: models.DeliveryFailed, wantReason: services.ReasonDeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := policy.Decide(tt.res, tt.attempt, tt.deadline, now)
			require.Equal(t, tt.wantState, d.State)
			if tt.wantReason != "" {
				require.Equal(t, tt.wantReason, *d.Reason)
			} else {
				require.Nil(t, d.Reason)
			}
			if tt.wantDue != 0 {
				require.Equal(t, now.Add(tt.wantDue), *d.DueAt)
			} else {
				require.Nil(t, d.DueAt)
			}
		})
	}
	t.Run("jitter stays within the fraction", func(t *testing.T) {
		d := services.DefaultPolicy.Decide(services.SendResult{StatusCode: 503}, 1, deadline, now)
		require.InDelta(t, 15.0, d.DueAt.Sub(now).Seconds(), 3.0)
	})
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	require.Equal(t, time.Duration(0), services.RetryAfter("", now))
	require.Equal(t, 30*time.Second, services.RetryAfter("30", now))
	require.Equal(t, time.Duration(0), services.RetryAfter("-5", now))
	require.Equal(t, time.Duration(0), services.RetryAfter("soon", now))
	require.Equal(t, time.Hour, services.RetryAfter(now.Add(time.Hour).UTC().Format(http.TimeFormat), now))
	require.Equal(t, time.Duration(0), services.RetryAfter(now.Add(-time.Hour).UTC().Format(http.TimeFormat), now))
}
