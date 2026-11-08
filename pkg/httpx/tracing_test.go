package httpx_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/bete7512/hookbeam/pkg/httpx"
)

func TestTracingNamesTheSpanAfterTheRoute(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := httpx.Chain(mux, httpx.Default(slog.New(slog.DiscardHandler), 1<<20)...)
	req := httptest.NewRequest(http.MethodGet, "/v1/things/7?secret=no", nil)
	req.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(httptest.NewRecorder(), req)
	spans := rec.Ended()
	require.Len(t, spans, 1)
	require.Equal(t, "GET /v1/things/{id}", spans[0].Name())
	require.Equal(t, "0af7651916cd43dd8448eb211c80319c", spans[0].SpanContext().TraceID().String(), "joins the caller's trace")
	for _, kv := range spans[0].Attributes() {
		require.NotContains(t, kv.Value.AsString(), "secret", "query strings are never attributes")
	}
}
