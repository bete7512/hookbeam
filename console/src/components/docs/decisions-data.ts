// The architecture decision records in docs/decisions, newest last. docs.test.ts compares this list with the
// folder, so a new decision appears here in the same commit.
export const decisions: { number: string; title: string; file: string }[] = [
  { number: "0001", title: "The OpenAPI spec is the only way to add a route", file: "0001-spec-first-api.md" },
  { number: "0002", title: "Two planes in one repository, one module, own databases", file: "0002-two-planes-one-repository.md" },
  { number: "0003", title: "Identity is goauth, not our own code", file: "0003-identity-through-goauth.md" },
  { number: "0004", title: "Workspaces are goauth organizations; API keys and scopes are Hookbeam's", file: "0004-workspaces-and-api-keys.md" },
  { number: "0005", title: "Events leave through a transactional outbox to SNS/SQS", file: "0005-outbox-over-sns-sqs.md" },
  { number: "0006", title: "SQL lives only in repos, hand-written with pgx", file: "0006-sql-only-in-repos.md" },
  { number: "0007", title: "Hot state lives in an in-process cache, never in Postgres", file: "0007-hot-state-in-cache-not-postgres.md" },
  { number: "0008", title: "Ids are `BIGINT` identity, `int64` on the wire", file: "0008-bigint-ids.md" },
  { number: "0009", title: "The planes talk over gRPC with mTLS; the workspace is trusted metadata", file: "0009-grpc-contract-and-mtls.md" },
  { number: "0010", title: "Postgres is the delivery schedule; the queue only wakes workers", file: "0010-postgres-is-the-schedule.md" },
  { number: "0011", title: "The payload is stored as the exact submitted bytes", file: "0011-payload-is-exact-bytes.md" },
  { number: "0012", title: "Signatures follow Standard Webhooks; secrets are encrypted at rest", file: "0012-standard-webhooks-and-secrets-at-rest.md" },
  { number: "0013", title: "Outbound requests resolve-then-dial through an SSRF guard, no redirects", file: "0013-outbound-ssrf-guard.md" },
  { number: "0014", title: "The retry policy is the spec's fixed table, decided after the attempt", file: "0014-retry-policy-fixed-table.md" },
  { number: "0015", title: "The delivery schema follows the spec; environments are deferred", file: "0015-schema-follows-spec-environments-deferred.md" },
  { number: "0016", title: "Claims are serialized by one advisory lock and capped per endpoint", file: "0016-claims-serialized-and-capped.md" },
  { number: "0017", title: "One queue per worker, filtered by event type; the control plane projects delivery state", file: "0017-one-queue-per-worker-projection-in-control-plane.md" },
  { number: "0018", title: "Endpoint health episodes are decided in the data plane's outcome transaction", file: "0018-health-episodes-in-the-data-plane.md" },
  { number: "0019", title: "Telemetry leaves every process through a collector", file: "0019-telemetry-through-a-collector.md" },
  { number: "0020", title: "Recurring jobs run in-process, exclusive ones behind an advisory lock", file: "0020-recurring-jobs-in-process-behind-advisory-locks.md" },
];
