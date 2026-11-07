# 0007. Hot state lives in an in-process cache, never in Postgres

Date: 2026-09-17 · Status: accepted

## Context
Login rate limits were first counted in a Postgres table. Every limited request became a write on the primary, so an attack scaled its own cost.

## Options
- **Keep counters in a Postgres table**: shared across replicas. Rejected: every limited request becomes a write on the primary, so a login flood turns into a write flood on the same rows.
- **Redis**: shared, fast, the usual answer. Rejected for now: a service nothing else needed; it can sit behind the same interface later.
- **One in-process cache (ristretto) per replica**: no network hop, no writes; each replica counts on its own. Chosen.

## Decision
Counters and other per-request state live in one in-process cache (`Core.Cache`). A rate limit exists to slow abuse, not to be an exact quota, so each replica counting on its own is acceptable.

## Consequences
- There is never a second cache instance or a package-level map.
- A Redis adapter can sit behind the same interface later if limits must be exact across replicas.
- Delivery rate and concurrency budgets are a scheduling problem in the delivery database, not a job for this cache.
