package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/logger"
)

// ContentTypeJSON is the Content-Type of every JSON response.
const ContentTypeJSON = "application/json; charset=utf-8"

// ErrorResponse is the body of every error response.
type ErrorResponse struct {
	Error     string            `json:"error"`
	Message   string            `json:"message"`
	Code      int               `json:"code"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	_, err = w.Write(b)
	return err
}

// WriteError writes err as an ErrorResponse and attaches it to the request log.
// Errors that are not an *apperr.AppError become a 500 without leaking their text.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *apperr.AppError
	if !errors.As(err, &appErr) {
		appErr = apperr.NewInternal(err)
	}
	recordError(r.Context(), err)
	if appErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(appErr.RetryAfter.Seconds()))))
	}
	_ = WriteJSON(w, appErr.Code, ErrorResponse{
		Error:     appErr.ApiCode,
		Message:   appErr.Message,
		Code:      appErr.Code,
		Fields:    appErr.Fields,
		RequestID: logger.RequestIDFrom(r.Context()),
	})
}

// DecodeJSON reads one JSON value into v, rejecting unknown fields and trailing data.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return bodyError(err, "invalid request body")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing data after JSON value")
		}
		return bodyError(err, "invalid request body")
	}
	return nil
}

// ReadBody returns the raw body for handlers that forward it byte for byte; over the limit is 413.
func ReadBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, bodyError(err, "unreadable request body")
	}
	return body, nil
}

// bodyError maps a read or decode failure: over the limit is 413, anything else 400.
func bodyError(err error, msg string) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &apperr.AppError{Code: http.StatusRequestEntityTooLarge, ApiCode: "PAYLOAD_TOO_LARGE", Message: "request body is too large", Err: err}
	}
	return apperr.NewBadRequest(msg, err)
}
