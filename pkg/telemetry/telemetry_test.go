package telemetry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// TestMetricsCarryOnlyAllowedAttributes records through every instrument method and checks the labels that come
// out: none may be a tenant, message, delivery or endpoint id.
func TestMetricsCarryOnlyAllowedAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	m, err := telemetry.NewMetrics()
	require.NoError(t, err)
	ctx := context.Background()
	m.Accepted(ctx, false)
	m.Accepted(ctx, true)
	m.Rejected(ctx, "validation")
	m.Attempt(ctx, "succeeded", 204, "")
	m.Attempt(ctx, "retry_scheduled", 0, "timeout")
	m.Retry(ctx)
	m.Exhaustion(ctx, "attempts_exhausted")
	m.Claimed(ctx, 7)
	m.Published(ctx, "delivery.due", 3)
	m.Handled(ctx, "delivery.succeeded", "ok")
	require.NoError(t, telemetry.Gauge("hookbeam.test.gauge", "test", func(context.Context) (float64, error) { return 1, nil }))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var seen int
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			seen++
			for _, kv := range attributesOf(met.Data) {
				require.Truef(t, telemetry.AllowedAttributeKeys[kv], "metric %s carries attribute %q", met.Name, kv)
			}
		}
	}
	require.GreaterOrEqual(t, seen, 9, "every instrument exported")
}

func attributesOf(data metricdata.Aggregation) []string {
	var keys []string
	switch d := data.(type) {
	case metricdata.Sum[int64]:
		for _, p := range d.DataPoints {
			for _, kv := range p.Attributes.ToSlice() {
				keys = append(keys, string(kv.Key))
			}
		}
	case metricdata.Histogram[int64]:
		for _, p := range d.DataPoints {
			for _, kv := range p.Attributes.ToSlice() {
				keys = append(keys, string(kv.Key))
			}
		}
	case metricdata.Gauge[float64]:
		for _, p := range d.DataPoints {
			for _, kv := range p.Attributes.ToSlice() {
				keys = append(keys, string(kv.Key))
			}
		}
	}
	return keys
}

func TestResponseClass(t *testing.T) {
	require.Equal(t, "http_2xx", telemetry.ResponseClass(204, ""))
	require.Equal(t, "http_5xx", telemetry.ResponseClass(503, ""))
	require.Equal(t, "timeout", telemetry.ResponseClass(0, "timeout"))
	require.Equal(t, "unknown", telemetry.ResponseClass(0, ""))
}

// TestTraceParentLinks writes a span's parent and starts a consumer span linked to it, as the queue path does.
func TestTraceParentLinks(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	_, err := telemetry.New(context.Background(), telemetry.Config{ServiceName: "test"})
	require.NoError(t, err)
	otel.SetTracerProvider(tp) // New installed its own provider; the recorder's is wanted here

	ctx, producer := telemetry.Tracer().Start(context.Background(), "producer")
	parent := telemetry.TraceParent(ctx)
	producer.End()
	require.Contains(t, parent, "00-", "a W3C traceparent")
	require.Empty(t, telemetry.TraceParent(context.Background()), "no span, no parent")

	_, consumer := telemetry.StartLinked(context.Background(), "consumer", parent, "", "garbage")
	consumer.End()
	spans := rec.Ended()
	require.Len(t, spans, 2)
	require.Len(t, spans[1].Links(), 1, "one link from the valid parent only")
	require.Equal(t, spans[0].SpanContext().TraceID(), spans[1].Links()[0].SpanContext.TraceID())
	_, ok := telemetry.LinkTo("not-a-traceparent")
	require.False(t, ok)
}
