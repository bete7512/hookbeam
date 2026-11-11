---
name: add-job
description: Use when a request or service must trigger background work in this repo (send an email, write an audit row, call another system) — adding a new event type to the outbox and a worker handler for it. Covers the event constant and payload, writing the outbox row in the producing transaction, the handler, registration, tests and the live LocalStack + Mailpit check.
---

# add-job

## How events work here

- A service writes the event in the same transaction as the change that caused it: `deps.Tx.WithTx(ctx, func(ctx, tx) { deps.Endpoints.WithTx(tx).CreateEndpoint(...); deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type, Payload}) })`.
- Account emails (welcome, verification, reset) are goauth's, sent from its own event bus; they are not outbox events. The outbox carries Hookbeam domain events only.
- The worker (`api/cmd/worker`) runs three goroutines: `publishEvents` calling `services.EventService.Publish` every `OUTBOX_POLL_INTERVAL` (drains unpublished `outbox` rows batch by batch with SKIP LOCKED, publishes them, marks `published_at`), `cleanOutbox` calling `CleanOutbox` hourly (deletes published rows older than `OUTBOX_RETENTION`), and the subscriber feeding `jobs.Handle`.
- `Jobs.Handle` (the `events.Handler` the subscriber calls) calls `services.EventService.ProcessOnce(ctx, e.ID, fn)`: one transaction marks `processed_events` (`false` = redelivery, handler skipped) and runs the handler; a handler error rolls the mark back and the queue redelivers (three times, then the dead-letter queue).
- Handlers depend on services and ports (`services.XService`, `mailer.Mailer`), never on repos or a transaction. Their writes commit on their own, so a handler must be safe to run again after a failure.
- Event types and payload structs live in `api/events`; the transport envelope is `pkg/events.Event{ID, Type, OccurredAt, Payload}`.
- SQL for `outbox` and `processed_events` lives only in `api/repos/{outbox,processed_events}.go`. `pkg/` has no SQL.

## Steps

1. **Declare the event** in `api/events/events.go`:
   ```go
   const EndpointPaused = "endpoint.paused"

   // EndpointPausedPayload is the payload of EndpointPaused.
   type EndpointPausedPayload struct {
   	EndpointID  int64  `json:"endpoint_id"`
   	WorkspaceID string `json:"workspace_id"` // goauth organization id
   }
   ```
   Payloads carry ids, not whole rows; the handler reads current data through a repo.
2. **Write it in the producing service**, inside the service's `WithTx` block:
   ```go
   payload, err := json.Marshal(apievents.XPayload{...})
   if err != nil { return err }
   return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.X, Payload: payload})
   ```
   A service that has no `Tx`/`Outbox` yet adds them to its `XServiceDeps` and the nil check, and each `cmd/*/main.go` `build` that constructs the service passes `tx` and `outboxRepo`.
3. **Handle it** in `api/jobs/<name>.go` as a `jobs.Handler` built over its own deps:
   ```go
   // NewX returns the handler for apievents.X.
   func NewX(endpoints services.EndpointService, mail mailer.Mailer) Handler {
   	return func(ctx context.Context, e events.Event) error {
   		var p apievents.XPayload
   		if err := json.Unmarshal(e.Payload, &p); err != nil { return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err) }
   		endpoint, err := endpoints.GetEndpoint(ctx, p.EndpointID)
   		...
   	}
   }
   ```
   Return an error for failed processing, unknown event types and malformed payloads. Retain the message for configured DLQ redrive; acknowledgement would discard the recovery evidence.
4. **Register it** in `api/cmd/worker/main.go` in the `Handlers` map passed to `jobs.New`: `apievents.X: jobs.NewX(d.endpoints, mail),` (the worker's `build` returns the services a handler needs in its `deps`). `jobs.New` rejects an empty type or a nil handler.
5. **Tests**
   - Service: in the `services_test` suite, expect `s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any())` and assert `e.Type` and `e.Payload` with `s.JSONEq`; keep the case where the outbox call fails and the service returns 500.
   - Handler: `api/jobs/<name>_test.go`, a table over `service_mocks` and `mailermemory.New()`; assert the effect (recipient and subject for mail) and the decode error case.
   - `Jobs.Handle` (`jobs_test.go`) and `EventService` (`services/events_test.go`) already have tests; add a case only if you changed them.
6. **Gates**: `verify-change` steps 2–8 (`go generate` if an interface changed).

## Live check

```bash
make db-up queue-up migrate-up
# in two terminals, with EVENTS_BACKEND=aws and MAILER_BACKEND=smtp in .env (copy the block from .env.example):
make run-api
make run-worker
# call the producing endpoint (log in first: POST /v1/auth/signup, POST /v1/auth/login → data.access_token), then:
curl -s localhost:8025/api/v1/messages | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["total"], [m["Subject"] for m in d["messages"]])'
```

- Expect the handler's effect within about a second, one `outbox` row with `published_at` set, and one `processed_events` row. The welcome email from signup is goauth's and arrives without the worker.
- Replay: publish the same envelope again with `docker compose --profile queue exec localstack awslocal sns publish --topic-arn arn:aws:sns:us-east-1:000000000000:hookbeam-events --message '<body>'`; the worker logs `event already processed` and no second mail arrives.
- Poison: publish an envelope whose handler fails (unknown user id); after three deliveries (about 90s with the 30s visibility timeout) it is in `hookbeam-worker-dlq`.
- `make queue-down` when done.

## Do not

- No SQL in `jobs/`, `services/` or `pkg/`; new tables get a repo in `api/repos`.
- No repos in `jobs/`; a handler that needs data gets it through a service method, adding one if it is missing.
- No publishing from handlers or business services; only the outbox row, `EventService.Publish` publishes.
- No handler that assumes it runs at most once; the mark and the handler's own writes are separate transactions.
- No new transport-specific code in `jobs/`; handlers see only `events.Event`.
