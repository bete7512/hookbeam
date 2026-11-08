package grpcx_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

func TestFromStatus(t *testing.T) {
	require.NoError(t, grpcx.FromStatus(nil))
	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantMsg    string
		wantFields []string
	}{
		{name: "round trip not found", err: grpcx.Status(apperr.NewNotFound("endpoint", nil)), wantCode: 404, wantMsg: "endpoint not found"},
		{name: "round trip conflict", err: grpcx.Status(apperr.NewConflict("name taken", nil)), wantCode: 409, wantMsg: "name taken"},
		{name: "round trip validation keeps fields", err: grpcx.Status(apperr.NewValidation(map[string]string{"url": "bad"})), wantCode: 422, wantFields: []string{"url"}},
		{name: "invalid argument without details", err: status.Error(codes.InvalidArgument, "nope"), wantCode: 400, wantMsg: "nope"},
		{name: "unauthenticated", err: status.Error(codes.Unauthenticated, "who"), wantCode: 401},
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "no"), wantCode: 403},
		{name: "resource exhausted", err: status.Error(codes.ResourceExhausted, "slow"), wantCode: 429},
		{name: "failed precondition is gone", err: status.Error(codes.FailedPrecondition, "payload has expired"), wantCode: 410},
		{name: "unavailable", err: status.Error(codes.Unavailable, "down"), wantCode: 503},
		{name: "deadline", err: status.Error(codes.DeadlineExceeded, "slow"), wantCode: 503},
		{name: "unimplemented is internal", err: status.Error(codes.Unimplemented, "later"), wantCode: 500},
		{name: "plain error is internal", err: errors.New("boom"), wantCode: 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var appErr *apperr.AppError
			require.ErrorAs(t, grpcx.FromStatus(tt.err), &appErr)
			require.Equal(t, tt.wantCode, appErr.Code)
			if tt.wantMsg != "" {
				require.Equal(t, tt.wantMsg, appErr.Message)
			}
			for _, f := range tt.wantFields {
				require.Contains(t, appErr.Fields, f)
			}
		})
	}
}
