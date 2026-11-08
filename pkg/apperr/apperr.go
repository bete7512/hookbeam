// Package apperr defines the application's error types.
// Repos wrap sentinels with %w, services build an *AppError, and
// httpx.WriteError turns it into a response.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Sentinel errors checked with errors.Is.
var (
	ErrNotFound     = errors.New("resource not found")
	ErrConflict     = errors.New("resource already exists")
	ErrValidation   = errors.New("validation failed")
	ErrBadRequest   = errors.New("bad request")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrUnavailable  = errors.New("service unavailable")
	ErrRateLimited  = errors.New("rate limited")
	ErrGone         = errors.New("gone")
	ErrInternal     = errors.New("internal server error")
)

// API codes returned to clients in the "error" field.
const (
	CodeNotFound     = "RESOURCE_NOT_FOUND"
	CodeConflict     = "ALREADY_EXISTS"
	CodeValidation   = "VALIDATION_FAILED"
	CodeBadRequest   = "BAD_REQUEST"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeUnavailable  = "SERVICE_UNAVAILABLE"
	CodeRateLimited  = "RATE_LIMITED"
	CodeGone         = "GONE"
	CodeInternal     = "INTERNAL_ERROR"
)

// AppError is an error with everything needed to answer an HTTP request.
type AppError struct {
	Code       int               // HTTP status
	ApiCode    string            // machine-readable code for clients
	Message    string            // client-safe message
	Err        error             // internal chain, for logs only
	Fields     map[string]string // field-level validation details
	RetryAfter time.Duration     // when set, sent as the Retry-After header
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

// NewNotFound reports a missing resource, e.g. NewNotFound("user", err).
func NewNotFound(resource string, err error) *AppError {
	return &AppError{Code: http.StatusNotFound, ApiCode: CodeNotFound, Message: resource + " not found", Err: chain(ErrNotFound, err)}
}

// NewConflict reports a state conflict such as a duplicate key.
func NewConflict(message string, err error) *AppError {
	return &AppError{Code: http.StatusConflict, ApiCode: CodeConflict, Message: message, Err: chain(ErrConflict, err)}
}

// NewValidation reports invalid input, one message per field.
func NewValidation(fields map[string]string) *AppError {
	return &AppError{Code: http.StatusUnprocessableEntity, ApiCode: CodeValidation, Message: "validation failed", Err: ErrValidation, Fields: fields}
}

// NewBadRequest reports a malformed request.
func NewBadRequest(message string, err error) *AppError {
	return &AppError{Code: http.StatusBadRequest, ApiCode: CodeBadRequest, Message: message, Err: chain(ErrBadRequest, err)}
}

// NewUnauthorized reports missing or invalid credentials.
func NewUnauthorized(message string, err error) *AppError {
	return &AppError{Code: http.StatusUnauthorized, ApiCode: CodeUnauthorized, Message: message, Err: chain(ErrUnauthorized, err)}
}

// NewForbidden reports valid credentials without permission.
func NewForbidden(message string, err error) *AppError {
	return &AppError{Code: http.StatusForbidden, ApiCode: CodeForbidden, Message: message, Err: chain(ErrForbidden, err)}
}

// NewUnavailable reports a dependency that is down.
func NewUnavailable(message string, err error) *AppError {
	return &AppError{Code: http.StatusServiceUnavailable, ApiCode: CodeUnavailable, Message: message, Err: chain(ErrUnavailable, err)}
}

// NewTooManyRequests reports a rate limit; retryAfter tells the client when to try again.
func NewTooManyRequests(retryAfter time.Duration) *AppError {
	return &AppError{Code: http.StatusTooManyRequests, ApiCode: CodeRateLimited, Message: "too many requests", Err: ErrRateLimited, RetryAfter: retryAfter}
}

// NewGone reports a resource that existed but was removed on purpose, such as an expired payload.
func NewGone(message string, err error) *AppError {
	return &AppError{Code: http.StatusGone, ApiCode: CodeGone, Message: message, Err: chain(ErrGone, err)}
}

// NewInternal hides err from the client behind a generic message.
func NewInternal(err error) *AppError {
	return &AppError{Code: http.StatusInternalServerError, ApiCode: CodeInternal, Message: "internal server error", Err: chain(ErrInternal, err)}
}

// chain puts sentinel in front of err so errors.Is finds it, without duplicating it.
func chain(sentinel, err error) error {
	if err == nil {
		return sentinel
	}
	if errors.Is(err, sentinel) {
		return err
	}
	return fmt.Errorf("%w: %w", sentinel, err)
}
