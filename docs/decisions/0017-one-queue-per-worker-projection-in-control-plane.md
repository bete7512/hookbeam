# 0017. One queue per worker, filtered by event type; the control plane projects delivery state

Date: 2026-11-08 · Status: accepted

## Context
The data plane announces every delivery outcome on the bus so the control plane can show health and send notifications. Both workers consumed the same SQS queue, so wake-ups and state events were split between them at random.

## Options
- **One shared queue, every worker ignores what is not its own**: Rejected: a message goes to one consumer, so each worker would swallow or dead-letter the other's events.
- **The control plane reads health straight from the data plane on every request**: Rejected: a cross-plane read on the hot path, and no place to count a failure streak over time.
- **One SQS queue per worker, each subscribed to the one SNS topic with a filter on the event type attribute; the control plane keeps a projection table**: Chosen.

## Decision
The publisher sets a `type` attribute on every message; each worker's subscription filters on it. The control plane handles delivery state events once each (processed-events table) and keeps one `endpoint_health` row per endpoint, updated only by newer observations.

## Consequences
- Adding a consumer is a new queue and a filter, never a change in the producer.
- The control plane has its own copy of the event names and payload shape; the two must stay compatible by hand.
- Bulk transitions in the data plane do not emit events yet, so the projection can lag behind an archive.
