# 0005. Events leave through a transactional outbox to SNS/SQS

Date: 2026-09-16 · Status: accepted

## Context
A committed change must trigger background work. The work must not be lost when the worker or broker is down, and must not run twice when the queue redelivers.

## Options
- **Publish from the request handler after the commit**: simplest. Rejected: a crash between the commit and the publish loses the event, and a publish before the commit announces a change that may roll back.
- **Postgres as the queue**: a jobs table polled by the worker, no broker. Rejected: the worker competes with the API for connections, fan-out needs more tables, and it does not match the AWS deployment.
- **Transactional outbox**: the event row is written in the same transaction as the change, a relay publishes it to SNS, the worker consumes from SQS. Chosen.

## Decision
The event is written as an outbox row in the same transaction as the change. The worker relays outbox rows to SNS and consumes them from SQS. Each consumer records the event id in `processed_events` inside its own transaction, so a redelivery does nothing.

## Consequences
- Services never publish directly; they write an outbox row and the relay publishes it.
- A handler that keeps failing is redelivered three times and then parked in the dead-letter queue.
- The transport is two small interfaces, so another at-least-once broker can replace SNS/SQS.
