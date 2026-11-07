# 0011. The payload is stored as the exact submitted bytes

Date: 2026-11-07 · Status: accepted

## Context
The signature covers the exact body the receiver gets. A first version stored the body as `JSONB`, and the receiver got re-serialized JSON with reordered keys.

## Options
- **`JSONB`**: lets SQL query inside the payload. Rejected: Postgres normalizes key order, whitespace and numbers, so the bytes the receiver gets are not the bytes the sender signed for.
- **Object storage from day one**: bodies in S3, a reference in the row. Rejected: a second store and lifecycle for bodies that fit in a row under the 256 KiB cap.
- **`BYTEA` inline**: the exact bytes, validated as JSON on the way in. Chosen.

## Decision
The payload column is `BYTEA`. The control plane forwards the raw request body, the data plane checks that it is UTF-8 JSON under 256 KiB, and stores it untouched along with its SHA-256.

## Consequences
- Messages are filtered by metadata parsed at submit time (`type`, `external_id`), never by querying inside the payload.
- If bodies grow beyond a row, a reference to object storage is a later decision.
