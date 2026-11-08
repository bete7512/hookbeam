# 0019 Telemetry leaves every process through a collector

## Context

Both planes and both workers need traces that follow one submission across the HTTP request, the gRPC call, the
outbox, the queue and the attempt, plus metrics an operator can alert on. A delivery platform must not expose a
scrape endpoint on the public listener, and metric labels must stay bounded: a label per workspace, message or
endpoint would grow without limit and put tenant ids into the metrics store.

## Options

1. Prometheus scraping each process on `/metrics`, with traces sent separately.
2. OpenTelemetry in every process, exported over OTLP to a collector that forwards to Tempo and a metrics store;
   trace context carried across the queue in the event envelope and linked, not parented.
3. Logs only, with ids to join by hand.

## Decision

Option 2. `pkg/telemetry` installs the global providers from `OTEL_EXPORTER_OTLP_ENDPOINT`; without it telemetry
stays in-process. The HTTP middleware and the gRPC stats handlers create the server and client spans; the outbox
stores the producing transaction's `trace_parent` and messages store the acceptance's, so the relay, the consumers
and the attempts start spans linked to them. Every metric instrument lives in `telemetry.Metrics` and takes only
bounded values; a unit test over the exported registry enforces the allowed label set. The profiler is served on
a separate internal listener behind a bearer token, never on the public one.

## Consequences

One trace shows submit → accept → relay → wake-up → attempt through links, which Tempo and Grafana display; a
single parent chain was never possible across the queue. The collector is one more container locally and a
sidecar in ECS. Metric names are OpenTelemetry's and become Prometheus names at the collector. Payload bytes,
headers and secrets never reach spans by construction, and a canary test keeps it that way.
