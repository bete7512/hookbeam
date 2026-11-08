// Package telemetry wires OpenTelemetry for a Hookbeam process: traces and metrics exported over OTLP to a
// collector when OTEL_EXPORTER_OTLP_ENDPOINT is set, otherwise kept in-process and dropped. It also carries
// trace context across the queue: a producer writes its trace parent next to the event, a consumer starts a
// span linked to it, so one trace follows a submission from the HTTP request to the attempt.
package telemetry

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

// Config names the process and where its telemetry goes.
type Config struct {
	ServiceName string
	Version     string
	Env         string
	// Endpoint is the OTLP/HTTP collector base URL (http://otel-collector:4318); empty keeps telemetry local.
	Endpoint string
	// Interval is how often metrics are exported.
	Interval time.Duration
}

// Telemetry owns the providers; Shutdown flushes them.
type Telemetry struct {
	traces  *sdktrace.TracerProvider
	metrics *sdkmetric.MeterProvider
}

// New builds the providers, installs them as the globals and sets the W3C propagators.
func New(ctx context.Context, cfg Config) (*Telemetry, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName), semconv.ServiceVersion(cfg.Version), semconv.DeploymentEnvironmentName(cfg.Env)))
	if err != nil {
		return nil, err
	}
	traceOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	metricOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if cfg.Endpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint+"/v1/traces"))
		if err != nil {
			return nil, err
		}
		traceOpts = append(traceOpts, sdktrace.WithBatcher(exp))
		mexp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.Endpoint+"/v1/metrics"))
		if err != nil {
			return nil, err
		}
		interval := cfg.Interval
		if interval <= 0 {
			interval = 15 * time.Second
		}
		metricOpts = append(metricOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(interval))))
	}
	t := &Telemetry{traces: sdktrace.NewTracerProvider(traceOpts...), metrics: sdkmetric.NewMeterProvider(metricOpts...)}
	otel.SetTracerProvider(t.traces)
	otel.SetMeterProvider(t.metrics)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return t, nil
}

// Shutdown flushes and stops both providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return errors.Join(t.traces.Shutdown(ctx), t.metrics.Shutdown(ctx))
}

const scope = "github.com/bete7512/hookbeam"

// Tracer is the tracer every Hookbeam span comes from.
func Tracer() trace.Tracer { return otel.Tracer(scope) }

// TraceParent serialises the span in ctx as a W3C traceparent header value, or empty without a span.
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// LinkTo turns a stored traceparent into a span link; an empty or malformed value gives no link.
func LinkTo(traceParent string) (trace.Link, bool) {
	if traceParent == "" {
		return trace.Link{}, false
	}
	ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": traceParent})
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return trace.Link{}, false
	}
	return trace.Link{SpanContext: sc}, true
}

// StartLinked starts a span that links to the traces the parents name, for work picked up from a queue or a
// table rather than called directly.
func StartLinked(ctx context.Context, name string, parents ...string) (context.Context, trace.Span) {
	links := make([]trace.Link, 0, len(parents))
	for _, p := range parents {
		if l, ok := LinkTo(p); ok {
			links = append(links, l)
		}
	}
	return Tracer().Start(ctx, name, trace.WithLinks(links...), trace.WithSpanKind(trace.SpanKindConsumer))
}

// Int64 is a span attribute helper.
func Int64(key string, v int64) attribute.KeyValue { return attribute.Int64(key, v) }

// String is a span attribute helper.
func String(key, v string) attribute.KeyValue { return attribute.String(key, v) }
