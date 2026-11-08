# Repeated failures

**Fires when** `sum(increase(hookbeam_attempts_total{outcome="failed"}[10m])) > 5`.
**Dashboard panel**: Data plane › Attempts per minute by outcome (and by response class).

## Symptom

More than five attempts failed for good in ten minutes: 4xx answers, blocked destinations or exhausted retries.
A single misconfigured endpoint can produce them all; the response class panel says whether it is `http_4xx`
(the receiver rejects, often a wrong secret), `blocked_destination` (the URL points at a private address) or
exhaustion after 5xx and timeouts.

## Confirm

1. Data plane › Attempts per minute by response class.
2. Console › Events, outcome `failed`: the failing endpoint and its attempts' response preview explain the receiver's answer.
3. Console › the endpoint's health: an open episode names it; owners and admins were notified.

## Safe action

- Wrong secret at the receiver: rotate from the console with an overlap and update the receiver, or fix the receiver's copy; then replay the failed deliveries.
- Receiver bug: pause the endpoint until it is fixed, then resume; nothing is sent meanwhile.
- A destination that should never be reached: archive the endpoint.

## Escalation and impact

Failed deliveries are kept with their attempts and can be replayed while the payload is retained (7 days by default). If the cause is outside the stack (queue, database, network), escalate to whoever runs it with the panel and the log lines above.
