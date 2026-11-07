# 0010. Postgres is the delivery schedule; the queue only wakes workers

Date: 2026-11-07 · Status: accepted

## Context
A delivery retries for up to 24 hours with delays up to 12 hours, and must be pausable, cancellable and visible in the console. SQS delays cap at 15 minutes, a queued message cannot be cancelled, and duplicates happen.

## Options
- **The queue is the schedule**: one SQS message per attempt with a delay. Rejected: delays cap at 15 minutes so long backoffs need re-enqueue chains, a queued message cannot be cancelled or paused, and the database does not know when the next attempt is.
- **A scheduler product** (Temporal, river, a cron service): retries and timers off the shelf. Rejected: a second system of record for the very state this product exists to show.
- **Postgres holds `due_at`; workers claim due rows with `FOR UPDATE SKIP LOCKED` under a lease; the queue only carries wake-ups**: Chosen.

## Decision
`deliveries.due_at` in Postgres is the schedule. A worker claims due rows with `FOR UPDATE SKIP LOCKED`, takes a lease token, and every later write for that attempt is conditional on the token, so a stale worker cannot overwrite a newer attempt. Queue messages are only wake-ups: a woken worker claims whatever is due, so a duplicate or late message is harmless.

## Consequences
- A delivery is `queued`, `in_flight`, `retry_scheduled`, `succeeded`, `failed` or `cancelled`, with a `cancel_requested` flag.
- A sweep returns expired leases to the schedule and claims overdue work whose wake-up was lost.
- The claim query is a hot path on one table, so its indexes and fairness need attention as volume grows.
