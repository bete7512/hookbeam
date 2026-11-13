# Notification pipeline failing

**Fires when** handled events with `result="error"` in ten minutes, or the control-plane worker's outbox lag over a minute, or its gauge gone, for one minute.
**Dashboard panel**: Control plane › Events handled per minute and Outbox lag (worker).

## Symptom

The control plane worker turns delivery state events into the health projection and health changes into notices
and emails. Handler errors (database down, the data plane unreachable for the episode reread, the mailer
refusing) leave the event in the queue for redelivery and, after three receives, in the DLQ; a stopped worker
stops the whole pipeline, which shows as a missing gauge.

## Confirm

1. Control plane › Events handled per minute, `result=error`, and the worker's logs for `event handler failed`.
2. The DLQ depth (`hookbeam-worker-dlq` locally; the CloudWatch alarm in AWS).
3. `docker compose ps api-worker`.

## Safe action

- Fix the cause (database, data plane, mailer) and redrive the DLQ; handlers are idempotent (`processed_events`), so redelivery is safe.
- Worker down: start it; the queue kept the events.

## Escalation and impact

Notices and alert emails lag; deliveries and the health badges, which come from the data plane, are unaffected. If the cause is outside the stack (queue, database, network), escalate to whoever runs it with the panel and the log lines above.
