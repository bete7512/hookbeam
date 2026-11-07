# 0006. SQL lives only in repos, hand-written with pgx

Date: 2026-09-16 · Status: accepted

## Context
The plumbing tables (outbox, processed events) are needed by every service, which made shared SQL in `pkg/` tempting.

## Options
- **Shared SQL in `pkg/outbox`**: one implementation of the outbox for every service. Rejected: shared code would own tables inside each service's database and migrate them all at once.
- **An ORM or sqlc for the plumbing tables**: less hand-written SQL. Rejected: a second way to write queries next to the hand-written pgx repos.
- **A hand-written pgx repo per table inside each service**, duplicated where two services need the same table. Chosen.

## Decision
Every query lives in `<service>/repos`, written by hand with pgx. Hookbeam code uses no ORM, sqlc or query builder; GORM exists only inside goauth.

## Consequences
- Each service's whole schema is visible in its own `repos/` and `migrations/`.
- The outbox repo is copied into each service on purpose; the shared part is the service logic over the repo interfaces.
- Queries use explicit column lists and named arguments, and client input never reaches a SQL string.
