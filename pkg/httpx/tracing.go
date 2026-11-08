package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Tracing starts a server span per request from the incoming trace context, names it after the matched route
// once the mux has matched, and records the request duration by method, route and status. Nothing from the
// body or the headers other than the trace context is recorded.
func Tracing() Middleware {
	tracer := otel.Tracer("github.com/bete7512/hookbeam/pkg/httpx")
	duration, _ := otel.Meter("github.com/bete7512/hookbeam/pkg/httpx").Float64Histogram("http.server.request.duration", metric.WithUnit("s"))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, "HTTP "+r.Method, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()
			if e, ok := ctx.Value(logEntryKey{}).(*logEntry); ok {
				e.traceID = span.SpanContext().TraceID().String()
			}
			start := time.Now()
			rw, ok := w.(*responseRecorder)
			if !ok {
				rw = &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			}
			// The mux records the matched pattern on the request it is given, so that copy is the one read after.
			r = r.WithContext(ctx)
			next.ServeHTTP(rw, r)
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			if strings.Contains(route, " ") {
				span.SetName(route) // the pattern already names the method
			} else {
				span.SetName(r.Method + " " + route)
			}
			span.SetAttributes(attribute.String("http.request.method", r.Method), attribute.String("http.route", route), attribute.Int("http.response.status_code", rw.status))
			if rw.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(rw.status))
			}
			if duration != nil {
				duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("method", r.Method), attribute.String("route", route), attribute.String("status", strconv.Itoa(rw.status))))
			}
		})
	}
}
