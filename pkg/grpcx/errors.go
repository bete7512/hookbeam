package grpcx

import (
	"context"
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Status converts an error into a gRPC status error. AppErrors map by sentinel and keep their message;
// validation errors carry their fields as BadRequest details; anything else is Internal with a generic message.
func Status(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	}
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		return status.Error(codes.Internal, "internal error")
	}
	code := codes.Internal
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		code = codes.NotFound
	case errors.Is(err, apperr.ErrConflict):
		code = codes.AlreadyExists
	case errors.Is(err, apperr.ErrValidation), errors.Is(err, apperr.ErrBadRequest):
		code = codes.InvalidArgument
	case errors.Is(err, apperr.ErrUnauthorized):
		code = codes.Unauthenticated
	case errors.Is(err, apperr.ErrForbidden):
		code = codes.PermissionDenied
	case errors.Is(err, apperr.ErrUnavailable):
		code = codes.Unavailable
	case errors.Is(err, apperr.ErrRateLimited):
		code = codes.ResourceExhausted
	case errors.Is(err, apperr.ErrGone):
		code = codes.FailedPrecondition
	}
	message := appErr.Message
	if code == codes.Internal {
		message = "internal error"
	}
	st := status.New(code, message)
	if len(appErr.Fields) > 0 {
		violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(appErr.Fields))
		for field, desc := range appErr.Fields {
			violations = append(violations, &errdetails.BadRequest_FieldViolation{Field: field, Description: desc})
		}
		if detailed, err := st.WithDetails(&errdetails.BadRequest{FieldViolations: violations}); err == nil {
			st = detailed
		}
	}
	if appErr.RetryAfter > 0 {
		if detailed, err := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(appErr.RetryAfter)}); err == nil {
			st = detailed
		}
	}
	return st.Err()
}
