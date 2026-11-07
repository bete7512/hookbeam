# 0012. Signatures follow Standard Webhooks; secrets are encrypted at rest

Date: 2026-11-07 · Status: accepted

## Context
Receivers must verify who sent a delivery and that it was not altered. The worker needs the secret on every attempt, and a database dump must not reveal it.

## Options
- **Our own `Hookbeam-*` signature headers**: Rejected: every receiver has to write a bespoke verifier.
- **Standard Webhooks headers and HMAC-SHA256**: existing libraries verify them. Chosen.
- **Secrets in plaintext rows**: Rejected: a database dump yields every secret. A hash is not an option either, because the worker must produce the HMAC.
- **A KMS call per attempt**: Rejected: latency and a cloud dependency on the hot path.
- **AES-256-GCM under one application key, with the key id stored per row**: Chosen.

## Decision
Signatures follow Standard Webhooks: `webhook-id` is the message's public id, the timestamp is fresh on every attempt, and the signature is HMAC-SHA256 over id, timestamp and body. Secrets are stored encrypted with AES-256-GCM under one application key, and every active secret of an endpoint signs each attempt.

## Consequences
- Any Standard Webhooks library verifies Hookbeam deliveries, and rotation with overlap is just a second signature in the header.
- Rotating the application key is a job that re-encrypts rows carrying the old key id.
