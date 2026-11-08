# 0018. Endpoint health episodes are decided in the data plane's outcome transaction

Date: 2026-11-08 · Status: accepted

## Context
Health must be exact: three consecutive failed attempts open an episode, the next success closes it, and two workers recording outcomes for one endpoint at the same moment must not open two. The first projection lived in the control plane and counted events after the fact, so it could lag, double count a redelivered event, or disagree with what the data plane had stored.

## Options
- **Keep counting in the control plane from delivery state events**: Rejected: the count is eventually consistent and cannot be fenced against concurrent outcomes; it also cannot see the endpoint revision an attempt used.
- **Compute health on every read from the attempts table**: Rejected: a scan per endpoint read, and no stable episode to notify about or link to.
- **One `endpoint_health` row per endpoint updated inside the outcome transaction, plus `health_episodes` with at most one open per endpoint**: Chosen.

## Decision
The outcome transaction that records an attempt also upserts the endpoint's streak row. The upsert locks the row, so concurrent outcomes for one endpoint serialize; it is guarded by the endpoint revision, so an attempt planned under an older revision is ignored. When the streak reaches three the same transaction opens an episode (a partial unique index keeps it single) and writes `endpoint.health_changed` to the outbox; a success resolves the open episode and announces that too. Endpoint reads attach `unknown|healthy|unhealthy`, the last observation time and a stale flag after fifteen minutes.

## Consequences
- Health is as exact as the attempts and visible on every endpoint response; an attempt that reached no network (a lost lease, a cancellation) says nothing.
- The control plane's `endpoint_health` projection from 0017 is redundant and will go once notifications consume `endpoint.health_changed`.
- Thresholds are constants for now; making them per endpoint is a schema change on `endpoints`.
