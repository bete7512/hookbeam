# Decisions

One short file per architecture decision: context, options, decision, consequences. A changed decision is a new file that names what it replaces.

- [0001](0001-spec-first-api.md) The OpenAPI spec is the only way to add a route
- [0002](0002-two-planes-one-repository.md) Two planes in one repository, one module, own databases
- [0003](0003-identity-through-goauth.md) Identity is goauth, not our own code
- [0004](0004-workspaces-and-api-keys.md) Workspaces are goauth organizations; API keys and scopes are Hookbeam's
- [0005](0005-outbox-over-sns-sqs.md) Events leave through a transactional outbox to SNS/SQS
- [0006](0006-sql-only-in-repos.md) SQL lives only in repos, hand-written with pgx
- [0007](0007-hot-state-in-cache-not-postgres.md) Hot state lives in an in-process cache, never in Postgres
- [0008](0008-bigint-ids.md) Ids are `BIGINT` identity, `int64` on the wire
- [0009](0009-grpc-contract-and-mtls.md) The planes talk over gRPC with mTLS; the workspace is trusted metadata
- [0010](0010-postgres-is-the-schedule.md) Postgres is the delivery schedule; the queue only wakes workers
- [0011](0011-payload-is-exact-bytes.md) The payload is stored as the exact submitted bytes
- [0012](0012-standard-webhooks-and-secrets-at-rest.md) Signatures follow Standard Webhooks; secrets are encrypted at rest
- [0013](0013-outbound-ssrf-guard.md) Outbound requests go through an SSRF guard, no redirects
- [0014](0014-retry-policy-fixed-table.md) The retry policy is the spec's fixed table, decided after the attempt
- [0015](0015-schema-follows-spec-environments-deferred.md) The schema follows the spec; environments are deferred
- [0016](0016-claims-serialized-and-capped.md) Claims are serialized by one advisory lock and capped per endpoint
- [0017](0017-one-queue-per-worker-projection-in-control-plane.md) One queue per worker, filtered by event type; the control plane projects delivery state
- [0018](0018-health-episodes-in-the-data-plane.md) Endpoint health episodes are decided in the data plane's outcome transaction
