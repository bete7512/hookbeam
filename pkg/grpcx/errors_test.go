package grpcx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

func TestStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{name: "nil", err: nil, wantCode: codes.OK},
		{name: "not found", err: apperr.NewNotFound("endpoint", nil), wantCode: codes.NotFound, wantMsg: "endpoint not found"},
		{name: "conflict", err: apperr.NewConflict("name taken", nil), wantCode: codes.AlreadyExists, wantMsg: "name taken"},
		{name: "validation", err: apperr.NewValidation(map[string]string{"url": "must be https"}), wantCode: codes.InvalidArgument},
		{name: "bad request", err: apperr.NewBadRequest("bad", nil), wantCode: codes.InvalidArgument, wantMsg: "bad"},
		{name: "unauthorized", err: apperr.NewUnauthorized("no", nil), wantCode: codes.Unauthenticated, wantMsg: "no"},
		{name: "forbidden", err: apperr.NewForbidden("no", nil), wantCode: codes.PermissionDenied, wantMsg: "no"},
		{name: "unavailable", err: apperr.NewUnavailable("db", nil), wantCode: codes.Unavailable, wantMsg: "db"},
		{name: "rate limited", err: apperr.NewTooManyRequests(0), wantCode: codes.ResourceExhausted},
		{name: "internal hides its message", err: apperr.NewInternal(errors.New("pq: boom")), wantCode: codes.Internal, wantMsg: "internal error"},
		{name: "plain error is internal", err: errors.New("boom"), wantCode: codes.Internal, wantMsg: "internal error"},
		{name: "canceled", err: context.Canceled, wantCode: codes.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: codes.DeadlineExceeded},
		{name: "status passes through", err: status.Error(codes.Unimplemented, "later"), wantCode: codes.Unimplemented, wantMsg: "later"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := grpcx.Status(tt.err)
			if tt.wantCode == codes.OK {
				require.NoError(t, err)
				return
			}
			st, ok := status.FromError(err)
			require.True(t, ok)
			require.Equal(t, tt.wantCode, st.Code())
			if tt.wantMsg != "" {
				require.Equal(t, tt.wantMsg, st.Message())
			}
		})
	}
}

func TestStatusValidationDetails(t *testing.T) {
	st, ok := status.FromError(grpcx.Status(apperr.NewValidation(map[string]string{"url": "must be https"})))
	require.True(t, ok)
	require.Len(t, st.Details(), 1)
	br, ok := st.Details()[0].(*errdetails.BadRequest)
	require.True(t, ok)
	require.Equal(t, "url", br.GetFieldViolations()[0].GetField())
	require.Equal(t, "must be https", br.GetFieldViolations()[0].GetDescription())
}
