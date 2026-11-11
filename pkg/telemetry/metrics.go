package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// AllowedAttributeKeys are the only labels a Hookbeam metric may carry: never a workspace, message, delivery,
// endpoint or user id, which would make the series unbounded and leak tenants into the metrics store.
var AllowedAttributeKeys = map[string]bool{
	"result":  true,
	"reason":  true,
	"outcome": true,
	"class":   true,
	"type":    true,
	"method":  true,
	"route":   true,
	"status":  true,
}

// Metrics holds every instrument the services record into. Every method takes only bounded values, so the
// label rule above is kept by construction. A nil *Metrics records nothing.
type Metrics struct {
	accepted    metric.Int64Counter
	rejected    metric.Int64Counter
	attempts    metric.Int64Counter
	retries     metric.Int64Counter
	exhaustions metric.Int64Counter
	claimBatch  metric.Int64Histogram
	published   metric.Int64Counter
	handled     metric.Int64Counter
}

// NewMetrics creates the instruments on the global meter provider; main installs that provider first.
func NewMetrics() (*Metrics, error) {
	m := otel.Meter(scope)
	var err error
	out := &Metrics{}
	if out.accepted, err = m.Int64Counter("hookbeam.messages.accepted", metric.WithDescription("messages accepted, by result (accepted, duplicate)")); err != nil {
		return nil, err
	}
	if out.rejected, err = m.Int64Counter("hookbeam.messages.rejected", metric.WithDescription("submissions refused, by reason")); err != nil {
		return nil, err
	}
	if out.attempts, err = m.Int64Counter("hookbeam.attempts", metric.WithDescription("delivery attempts, by outcome and response class")); err != nil {
		return nil, err
	}
	if out.retries, err = m.Int64Counter("hookbeam.retries", metric.WithDescription("attempts that scheduled a retry")); err != nil {
		return nil, err
	}
	if out.exhaustions, err = m.Int64Counter("hookbeam.exhaustions", metric.WithDescription("deliveries that gave up, by reason")); err != nil {
		return nil, err
	}
	if out.claimBatch, err = m.Int64Histogram("hookbeam.claims.batch_size", metric.WithDescription("deliveries claimed per claim")); err != nil {
		return nil, err
	}
	if out.published, err = m.Int64Counter("hookbeam.events.published", metric.WithDescription("outbox events published, by type")); err != nil {
		return nil, err
	}
	if out.handled, err = m.Int64Counter("hookbeam.events.handled", metric.WithDescription("events handled by the worker, by type and result (ok, replay, error)")); err != nil {
		return nil, err
	}
	return out, nil
}

// Accepted counts a submission that was stored or recognised as a duplicate.
func (m *Metrics) Accepted(ctx context.Context, duplicate bool) {
	if m == nil {
		return
	}
	result := "accepted"
	if duplicate {
		result = "duplicate"
	}
	m.accepted.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

// Rejected counts a refused submission: validation, conflict, no_subscriber, backlog, not_found, unauthorized or error.
func (m *Metrics) Rejected(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.rejected.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// Attempt counts an attempt by the state it led to and the response class (http_2xx, http_5xx, timeout, ...).
func (m *Metrics) Attempt(ctx context.Context, outcome string, statusCode int, errorClass string) {
	if m == nil {
		return
	}
	m.attempts.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome), attribute.String("class", ResponseClass(statusCode, errorClass))))
}

// Retry counts an attempt that scheduled another.
func (m *Metrics) Retry(ctx context.Context) {
	if m == nil {
		return
	}
	m.retries.Add(ctx, 1)
}

// Exhaustion counts a delivery that gave up, by reason (attempts_exhausted, deadline_exceeded).
func (m *Metrics) Exhaustion(ctx context.Context, reason string) {
	if m == nil {
		return
	}
	m.exhaustions.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
}

// Claimed records how many deliveries one claim took.
func (m *Metrics) Claimed(ctx context.Context, n int) {
	if m == nil {
		return
	}
	m.claimBatch.Record(ctx, int64(n))
}

// Published counts outbox events the relay published.
func (m *Metrics) Published(ctx context.Context, eventType string, n int) {
	if m == nil {
		return
	}
	m.published.Add(ctx, int64(n), metric.WithAttributes(attribute.String("type", eventType)))
}

// Handled counts an event the worker handled: ok, replay or error.
func (m *Metrics) Handled(ctx context.Context, eventType, result string) {
	if m == nil {
		return
	}
	m.handled.Add(ctx, 1, metric.WithAttributes(attribute.String("type", eventType), attribute.String("result", result)))
}

// ResponseClass buckets an attempt's result: http_2xx..http_5xx by status, else the transport error class.
func ResponseClass(statusCode int, errorClass string) string {
	if statusCode > 0 {
		return fmt.Sprintf("http_%dxx", statusCode/100)
	}
	if errorClass == "" {
		return "unknown"
	}
	return errorClass
}

// Gauge registers an observable gauge in seconds or units whose value the callback reads on every export.
func Gauge(name, description string, read func(ctx context.Context) (float64, error)) error {
	g, err := otel.Meter(scope).Float64ObservableGauge(name, metric.WithDescription(description))
	if err != nil {
		return err
	}
	_, err = otel.Meter(scope).RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		v, err := read(ctx)
		if err != nil {
			return nil //nolint:nilerr // a failed read skips one sample; the next export tries again
		}
		o.ObserveFloat64(g, v)
		return nil
	}, g)
	return err
}
