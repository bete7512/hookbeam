package grpcx_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

func TestRetryAfterCrossesTheWire(t *testing.T) {
	err := grpcx.Status(apperr.NewTooManyRequests(30 * time.Second))
	st := status.Convert(err)
	require.Equal(t, codes.ResourceExhausted, st.Code())
	var found bool
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			found = true
			require.Equal(t, 30*time.Second, ri.GetRetryDelay().AsDuration())
		}
	}
	require.True(t, found, "a RetryInfo detail carries the delay")

	back := grpcx.FromStatus(err)
	var appErr *apperr.AppError
	require.ErrorAs(t, back, &appErr)
	require.Equal(t, 429, appErr.Code)
	require.Equal(t, 30*time.Second, appErr.RetryAfter)

	require.ErrorAs(t, grpcx.FromStatus(status.Error(codes.ResourceExhausted, "bare")), &appErr)
	require.Zero(t, appErr.RetryAfter, "no detail, no delay")
}

func TestUnavailableTellsTheClientWhenToRetry(t *testing.T) {
	var appErr *apperr.AppError
	require.ErrorAs(t, grpcx.FromStatus(status.Error(codes.Unavailable, "down")), &appErr)
	require.Equal(t, 503, appErr.Code)
	require.Equal(t, grpcx.UnavailableRetryAfter, appErr.RetryAfter)
	require.ErrorIs(t, appErr, apperr.ErrUnavailable)
	require.ErrorAs(t, grpcx.FromStatus(errors.Join(status.Error(codes.DeadlineExceeded, "slow"))), &appErr)
	require.Equal(t, grpcx.UnavailableRetryAfter, appErr.RetryAfter)
}
