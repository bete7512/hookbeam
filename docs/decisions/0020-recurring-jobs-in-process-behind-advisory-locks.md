# 0020 Recurring jobs run in-process, exclusive ones behind an advisory lock

## Context

Each worker has recurring work of two kinds. Drains keep up with a queue: relay the outbox, claim due deliveries,
sweep expired leases. Housekeeping belongs to the calendar: clean the outbox, prune audit rows, expire retained
payloads. The first version ran every job as a ticker loop in `main.go`, measured from process start, on every
replica. That gave no schedule to read, no count of runs, "hourly" that drifted with each deploy, and housekeeping
that two replicas would both do. A second worker replica on ECS makes the last point concrete.

## Options

1. Keep interval loops per replica and rely on `SKIP LOCKED` to make duplicate housekeeping harmless.
2. A scheduler that publishes a job message per tick to the queue, consumed like any other event, so recurring work
   gets the queue's retries and dead-letter path.
3. One in-process runner over a declared table of jobs: cadence jobs on every replica, calendar jobs by cron
   expression, and jobs that must run once across the fleet held behind a Postgres session advisory lock.
4. The same runner, coordinated through a table of its own: one row per job with a lease, its last run and a
   catch-up rule (never built).

## Decision

Option 3. `pkg/worker.Runner` runs a `[]worker.Job` declared in each worker's `main.go`: `Every` for drains, `At`
(a cron expression in `APP_TIMEZONE`) for housekeeping, `Exclusive` for work that must not run twice. An exclusive
job runs only on the replica whose `Locks.Hold(name)` succeeds; `repos.JobLockRepo` implements that with
`pg_try_advisory_lock` on one connection pinned from the pool, held across runs. The lock lives as long as the
connection, so a replica that dies hands over by itself, and nothing is written to the schema. Every run is counted
and timed in `hookbeam.jobs.runs` and `hookbeam.jobs.duration`, skipped runs included, so a job that stopped shows
as a flat line, not silence.

Option 2 was rejected because the queue is SNS/SQS: each tick would be paid network hops for work in the same
process, it would be the only publisher outside the outbox relay, and it would not stop N replicas from publishing N
ticks. Option 4 was rejected because coordination state is not business data: a lease row, a last-run column and a
catch-up rule reproduce what Postgres already does for a session lock, at the price of a table in each plane.

## Consequences

The job table in `main.go` is the schedule, readable in one place. Calendar jobs run at their ticks only, like cron;
a worker that is down across a tick runs that job at the next one, which housekeeping tolerates. Each worker pins
one pool connection for its locks. One replica does all the exclusive housekeeping of a plane; the others count
`skipped` runs. A holder whose connection drops without closing keeps its locks until Postgres notices the dead
session (TCP keepalive, minutes at most), and the job is skipped everywhere until then. The queue stays untouched by recurring work. An operator trigger ("run this job now") is a wake-up
on the job's channel and is not built yet.
