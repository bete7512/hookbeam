# 0014. The retry policy is the spec's fixed table, decided after the attempt

Date: 2026-11-07 · Status: accepted

## Context
The vertical slice needed a next state after a failed attempt, and the spec already fixes the defaults.

## Options
- **Per-endpoint configurable retry policy now**: Rejected: a policy model, API and validation before anyone asked for it.
- **Compute the whole schedule before the attempt**: Rejected: `Retry-After` is only known after the response.
- **One fixed policy, the spec's table, applied after the response is known**: Chosen.

## Decision
One fixed policy applies to every endpoint: eight attempts with delays of 15s, 1m, 5m, 30m, 2h, 6h and 12h plus ±20 % jitter, a 24-hour cycle, retries on network failure, 408, 425, 429 and 5xx, and a terminal result on other 4xx and all 3xx. `Retry-After` is honoured when it is later than the backoff. The decision is made after the response is known.

## Consequences
- A terminal delivery records the policy's reason, so the console can say why it ended.
- Tuning per endpoint within bounds is a later field on the endpoint.
