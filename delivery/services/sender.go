package services

import "context"

// SendRequest is one outbound webhook request.
type SendRequest struct {
	URL     string
	Headers map[string]string
	Body    []byte
}

// SendResult is what came back: a status (and headers, preview) or a classified transport failure.
type SendResult struct {
	StatusCode  int // 0 when no response was received
	Headers     map[string]string
	BodyPreview string
	// ErrorClass names the failure when StatusCode is 0: timeout, dns, connection, tls, blocked_destination.
	ErrorClass string
	Err        error
}

// Sender performs the HTTP request; delivery/sender implements it over the bounded client.
type Sender interface {
	Send(ctx context.Context, req SendRequest) SendResult
}
