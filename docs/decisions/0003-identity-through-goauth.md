# 0003. Identity is goauth, not our own code

Date: 2026-11-07 · Status: accepted · Replaces the hand-written auth of 2026-09-15/16

## Context
We had 7,400 lines of our own sessions, tokens, email flows and Google login, and Hookbeam still needed key rotation, cookies, organizations and invitations. All of it competed with the product for attention.

## Options
- **Keep our own code**: continue building sessions, tokens, email flows, Google login, organizations and invitations ourselves. Rejected: every identity feature competes with the product for attention.
- **goauth with a Postgres storage written here in pgx**: keeps the no-ORM rule end to end. Rejected: about a hundred repository methods plus transaction coordinators before anything works, and goauth warns that partial stores break its atomic flows.
- **goauth with its own GORM storage, opened over our pgx pool**: the library owns its tables and migrations; GORM is confined to them. Chosen.

## Decision
goauth owns identity. It runs over the service's pgx pool, its routes are mounted at `/v1/auth`, and GORM is confined to its tables. Our spec-driven middleware stays and calls `Auth.Authenticate`.

## Consequences
- goauth's ids are `VARCHAR(36)`; Hookbeam tables reference them as strings and never run SQL against goauth's tables.
- goauth's migrations run from the same `migrate up` command, and its OpenAPI document is merged into ours at startup.
- We depend on a beta library that the author maintains, so upgrades are tested live.
