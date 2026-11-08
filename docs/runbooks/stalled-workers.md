# Stalled workers

**Fires when** `max(hookbeam_deliveries_oldest_due_age) > 30 and (sum(rate(hookbeam_claims_batch_size_count[5m])) == 0 or absent(...))` for one minute.
**Dashboard panel**: Data plane › Claims per minute and Oldest due delivery age.

## Symptom

Deliveries are due but nobody claims: every delivery worker is down, cannot reach the database, or is stuck
holding the claim advisory lock (a crashed transaction releases it; a hung connection does not until its
timeout).

## Confirm

1. `docker compose ps delivery-worker` (in AWS the service's running count).
2. `docker compose logs --since 10m delivery-worker`: database errors, or nothing at all (hung).
3. In Postgres: `SELECT * FROM pg_locks WHERE locktype = 'advisory'` shows who holds the claim lock, and `pg_stat_activity` its connection.

## Safe action

- Start or restart the worker. On start it claims immediately, then on the timer.
- A hung lock holder: terminate its backend (`pg_terminate_backend`); the lock is released and the next claim proceeds.
- Never clear leases by hand; the sweep (`DELIVERY_CLAIM_SWEEP_INTERVAL`) re-leases expired ones and records what it could not know as `unknown`.

## Escalation and impact

Severity critical: nothing is delivered until a worker claims again. If the cause is outside the stack (queue, database, network), escalate to whoever runs it with the panel and the log lines above.
