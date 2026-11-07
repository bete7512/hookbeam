package grpcx

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

// FromStatus converts a gRPC error from another service into the matching AppError, so a handler
// can write it like any local error. Unknown codes and non-status errors become Unavailable or Internal.
func FromStatus(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return apperr.NewInternal(err)
	}
	msg := st.Message()
	switch st.Code() {
	case codes.OK:
		return nil
	case codes.NotFound:
		return apperr.NewNotFound(resourceOf(msg), err)
	case codes.AlreadyExists:
		return apperr.NewConflict(msg, err)
	case codes.InvalidArgument:
		fields := map[string]string{}
		for _, d := range st.Details() {
			if br, ok := d.(*errdetails.BadRequest); ok {
				for _, v := range br.GetFieldViolations() {
					fields[v.GetField()] = v.GetDescription()
				}
			}
		}
		if len(fields) > 0 {
			return apperr.NewValidation(fields)
		}
		return apperr.NewBadRequest(msg, err)
	case codes.Unauthenticated:
		return apperr.NewUnauthorized(msg, err)
	case codes.PermissionDenied:
		return apperr.NewForbidden(msg, err)
	case codes.ResourceExhausted:
		return apperr.NewTooManyRequests(0)
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return apperr.NewUnavailable("delivery service unavailable", err)
	default:
		return apperr.NewInternal(err)
	}
}

// resourceOf turns "endpoint not found" back into "endpoint" for NewNotFound.
func resourceOf(msg string) string {
	const suffix = " not found"
	if len(msg) > len(suffix) && msg[len(msg)-len(suffix):] == suffix {
		return msg[:len(msg)-len(suffix)]
	}
	if msg == "" {
		return "resource"
	}
	return msg
}
