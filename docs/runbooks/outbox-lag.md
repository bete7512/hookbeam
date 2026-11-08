# Outbox lag

**Fires when** `max(hookbeam_outbox_oldest_age) > 60` for one minute.
**Dashboard panel**: Control plane › Outbox lag (worker) and Data plane › Outbox lag (data plane).

## Symptom

An outbox event has waited more than a minute. The relay (`publishEvents` in each worker) drains the outbox every
`OUTBOX_POLL_INTERVAL`; it stops draining when the queue refuses publishes (SNS or LocalStack down, bad
credentials, a filter policy that rejects the attribute) or when the worker itself is down.

## Confirm

1. Which plane: the metric's `job` label says `worker` (control plane) or `delivery-worker` (data plane).
2. `docker compose logs --since 10m <worker>` and look for `publish events failed` lines; the error names the cause.
3. `curl -s localhost:4566/_localstack/health` locally; in AWS the SNS topic policy and the worker's role.

## Safe action

- Queue down: restore it; the relay resumes on its own and publishes the backlog in order. Nothing is lost: the rows stay in the outbox.
- Worker down: start it. A single replica is enough; several relays share the outbox safely (`FOR UPDATE SKIP LOCKED`).
- Never delete outbox rows by hand; `CleanOutbox` prunes published ones after `OUTBOX_RETENTION`.

## Escalation and impact

Wake-ups and state events are late while the lag lasts: deliveries still run on the claim timer, but health notices and emails wait. If the cause is outside the stack (queue, database, network), escalate to whoever runs it with the panel and the log lines above.
