# 0015. The delivery schema follows the spec; environments are deferred

Date: 2026-11-07 · Status: accepted

## Context
The spec describes the full state and retention model. Adding its columns later would mean an expand/contract migration every milestone.

## Options
- **The planning notes' shorter schema**, extended when each feature arrives. Rejected: an expand/contract migration per milestone on tables the spec already describes.
- **The full spec including environments**: Rejected: an environment model and API before a single message is delivered.
- **The full spec's columns now, environments inserted later by one migration**: Chosen.

## Decision
The baseline migration carries the spec's states, idempotency and payload expiry, endpoint revisions, leases, replay links and versioned secrets. Environments, the spec's level between workspace and application, are deferred and will be inserted later by one migration.

## Consequences
- Milestones 2 and 3 add behaviour, not columns.
- Adding environments is one migration plus the routes.
