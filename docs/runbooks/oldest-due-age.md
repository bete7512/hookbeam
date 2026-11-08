# Oldest due age

**Fires when** `max(hookbeam_deliveries_oldest_due_age) > 120` for one minute.
**Dashboard panel**: Data plane › Oldest due delivery age.

## Symptom

A queued or retry-scheduled delivery has been due for more than two minutes without being claimed. Claims run on
`DELIVERY_CLAIM_INTERVAL` and on every wake-up, capped by `DELIVERY_CLAIM_BATCH_SIZE` and the per-endpoint
concurrency, so the usual causes are a stopped worker, a worker busy with slow destinations (every slot taken),
or a batch far smaller than the arrival rate.

## Confirm

1. Data plane › Claims per minute: zero means no worker is claiming (see Stalled workers); nonzero means the workers cannot keep up.
2. Data plane › Attempts in flight against `DELIVERY_CLAIM_CONCURRENCY`: at the cap, destinations are slow (timeouts) and every slot waits.
3. `docker compose logs --since 10m delivery-worker` for `claim` warnings.

## Safe action

- Worker down: start it; the backlog drains oldest first per endpoint.
- Slots exhausted by one slow destination: pause that endpoint from the console (its deliveries hold without
  counting against the slots) and raise `DELIVERY_SENDER_TIMEOUT` only if the destination is genuinely slow.
- Sustained arrival above capacity: add a worker replica (claims are serialised by the advisory lock, so replicas are safe) or raise the batch size.

## Escalation and impact

Deliveries are late, not lost; the deadline (24 h) still applies. If the cause is outside the stack (queue, database, network), escalate to whoever runs it with the panel and the log lines above.
