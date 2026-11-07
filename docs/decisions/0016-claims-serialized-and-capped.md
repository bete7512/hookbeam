# 0016. Claims are serialized by one advisory lock and capped per endpoint

Date: 2026-11-08 · Status: accepted

## Context
One stalling destination must not starve the others, and the per-endpoint in-flight cap must hold across worker replicas. The claim query is the one place every worker goes through.

## Options
- **Count in-flight attempts inside the claim query only**: Rejected: two replicas claiming at the same moment both see the cap as free and exceed it.
- **Lock the endpoint rows while claiming**: Rejected: a window-function query cannot take row locks, and partial locks make the fairness ordering unpredictable.
- **One Postgres advisory lock around the claim transaction, with the cap and the per-endpoint ranking in the query**: Chosen.

## Decision
A claim is one short transaction that takes a single advisory lock, ranks due deliveries per endpoint, takes every endpoint's oldest before any second one, and stops at the cap minus the endpoint's in-flight count. The worker hands claimed rows to a bounded pool and returns at once, so a slow attempt never delays the next claim.

## Consequences
- Claims run one at a time across replicas; each is milliseconds, so this is not the bottleneck at this scale.
- The in-flight count needs its own partial index (migration 0002).
- A per-endpoint attempt rate still needs a separate mechanism.
