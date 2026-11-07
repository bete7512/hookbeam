# 0008. Ids are `BIGINT` identity, `int64` on the wire

Date: 2026-09-15 · Status: accepted (goauth exception 2026-11-07)

## Context
Every table needs one id type that works the same in SQL, Go and the spec.

## Options
- **UUID v4 or v7**: ids cannot be guessed and can be generated offline. Rejected: 16-byte keys, scattered index inserts (v4), unreadable in logs and URLs.
- **Bigint in the database but decimal strings on the wire**: protects JavaScript clients above 2^53. Rejected: converts every DTO and parameter for a limit these ids will never reach.
- **`BIGINT GENERATED ALWAYS AS IDENTITY` in SQL, `int64` in Go and in the spec**: Chosen.

## Decision
Hookbeam tables use `BIGINT GENERATED ALWAYS AS IDENTITY`, which is `int64` in Go and in the spec. goauth's tables keep their `VARCHAR(36)` ids, which Hookbeam references as strings.

## Consequences
- Ids can be guessed, so every read is scoped by workspace and a foreign row is "not found".
- Ids that are meant to be opaque, such as the `webhook-id` (`msg_…`), are strings by design.
