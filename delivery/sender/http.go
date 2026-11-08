// Package sender performs webhook requests over the bounded, destination-checked client.
package sender

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/safehttp"
)

// Config bounds what is kept from a response.
type Config struct {
	MaxResponseBytes int64 // read at most this much of the body
	MaxPreviewBytes  int   // store at most this much of it
}

// HTTPSender implements services.Sender.
type HTTPSender struct {
	client *http.Client
	cfg    Config
}

var _ services.Sender = (*HTTPSender)(nil)

// safeResponseHeaders are the receiver response headers an attempt records; anything else (cookies, authentication
// challenges, custom headers that may carry tokens) is dropped before it reaches the database or the console.
var safeResponseHeaders = map[string]bool{
	"content-type": true, "content-length": true, "content-encoding": true, "date": true, "server": true,
	"retry-after": true, "cache-control": true, "location": true, "vary": true, "via": true,
	"x-request-id": true, "x-correlation-id": true, "x-powered-by": true,
}

// New returns a sender over client, which should come from safehttp.
func New(client *http.Client, cfg Config) *HTTPSender {
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 64 << 10
	}
	if cfg.MaxPreviewBytes <= 0 {
		cfg.MaxPreviewBytes = 1 << 10
	}
	return &HTTPSender{client: client, cfg: cfg}
}

// Send posts the body with the headers and returns the classified result; it never returns a Go error for HTTP statuses.
func (s *HTTPSender) Send(ctx context.Context, req services.SendRequest) services.SendResult {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, strings.NewReader(string(req.Body)))
	if err != nil {
		return services.SendResult{ErrorClass: "connection", Err: err}
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := s.client.Do(httpReq) //nolint:bodyclose // closed below when present
	if resp == nil && err != nil {
		return services.SendResult{ErrorClass: classify(err), Err: err}
	}
	defer resp.Body.Close()
	res := services.SendResult{StatusCode: resp.StatusCode, Headers: make(map[string]string, len(resp.Header))}
	for k, v := range resp.Header {
		if k := strings.ToLower(k); safeResponseHeaders[k] && len(v) > 0 {
			res.Headers[k] = v[0]
		}
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxResponseBytes))
	res.BodyPreview = preview(body, s.cfg.MaxPreviewBytes)
	return res
}

// classify names a transport failure for the attempt record.
func classify(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var hostErr x509.HostnameError
	var netErr net.Error
	switch {
	case errors.Is(err, safehttp.ErrBlockedDestination):
		return "blocked_destination"
	case errors.As(err, &dnsErr):
		return "dns" // including a resolver timeout: the name, not the host, is the problem
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return "timeout"
	case errors.As(err, &certErr) || errors.As(err, &hostErr) || strings.Contains(err.Error(), "tls:"):
		return "tls"
	default:
		return "connection"
	}
}

// preview keeps the first max bytes as valid UTF-8 text, never ending mid-character.
func preview(body []byte, limit int) string {
	if len(body) > limit {
		body = body[:limit]
		for i := 0; i < utf8.UTFMax-1 && len(body) > 0; i++ {
			if r, _ := utf8.DecodeLastRune(body); r != utf8.RuneError {
				break
			}
			body = body[:len(body)-1]
		}
	}
	return strings.ToValidUTF8(string(body), "\uFFFD")
}
