// Both planes' schemas as the migrations leave them. src/components/docs/docs.test.ts parses
// api/migrations and delivery/migrations and fails when a table, column, type, nullability, key or index here
// differs from the database, so this file changes in the same commit as the migration.

export type Plane = "delivery" | "control";

export interface Column {
  name: string;
  type: string; // bigint, int, text, text[], varchar(36), timestamptz, bool, bytea, jsonb
  nullable?: boolean;
  pk?: boolean;
  fk?: string; // "table.column" in the same database: a real foreign key
  ref?: string; // "goauth.users" or "delivery.applications": an id kept by value, no foreign key
}

export interface Index {
  name: string;
  why: string;
}

export interface Table {
  name: string;
  plane: Plane;
  x: number;
  y: number;
  purpose: string;
  columns: Column[];
  indexes?: Index[];
}

const ts = "timestamptz";
const ws = "varchar(36)";

export const deliveryTables: Table[] = [
  {
    name: "environments",
    plane: "delivery",
    x: 0,
    y: 0,
    purpose: "A workspace's development or production space. Applications live in one, so production work is never one click from a test.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "name", type: "text" },
      { name: "kind", type: "text" },
      { name: "created_by", type: ws },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "applications",
    plane: "delivery",
    x: 0,
    y: 192,
    purpose: "One consumer of your webhooks, such as Customer A. It is the recipient boundary: a message is submitted to exactly one application and never crosses to another.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "environment_id", type: "bigint", fk: "environments.id" },
      { name: "name", type: "text" },
      { name: "created_by", type: ws },
      { name: "archived_at", type: ts, nullable: true },
      { name: "response_previews_enabled", type: "bool" },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
    indexes: [{ name: "applications_environment_id_name_key", why: "Names are unique per environment." }],
  },
  {
    name: "messages",
    plane: "delivery",
    x: 300,
    y: 268,
    purpose: "One submitted event. public_id is the webhook-id receivers deduplicate on; payload holds the exact submitted bytes, which are both the outbound body and what the signature covers.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "public_id", type: "text" },
      { name: "workspace_id", type: ws },
      { name: "application_id", type: "bigint", fk: "applications.id" },
      { name: "event_type", type: "text" },
      { name: "sender_event_id", type: "text", nullable: true },
      { name: "idempotency_key", type: "text", nullable: true },
      { name: "idempotency_expires_at", type: ts, nullable: true },
      { name: "payload", type: "bytea" },
      { name: "payload_sha256", type: "bytea" },
      { name: "payload_expires_at", type: ts },
      { name: "accepted_at", type: ts },
      { name: "trace_parent", type: "text" },
    ],
    indexes: [
      { name: "messages_idempotency_idx", why: "Unique key per application while the key is retained; the key is set to NULL when its window ends, so it can be used again." },
      { name: "messages_application_accepted_idx", why: "The event explorer's newest-first page." },
      { name: "messages_payload_expiry_idx", why: "The retention job finds payloads past payload_expires_at." },
      { name: "messages_key_expiry_idx", why: "Partial: only rows still holding a key, for the job that releases them." },
    ],
  },
  {
    name: "endpoints",
    plane: "delivery",
    x: 300,
    y: 0,
    purpose: "A URL that receives an application's webhooks. revision increases with every configuration change; each delivery records the revision it was planned under and each attempt the one it used.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "application_id", type: "bigint", fk: "applications.id" },
      { name: "url", type: "text" },
      { name: "description", type: "text" },
      { name: "revision", type: "int" },
      { name: "paused_at", type: ts, nullable: true },
      { name: "archived_at", type: ts, nullable: true },
      { name: "created_by", type: ws },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "endpoint_secrets",
    plane: "delivery",
    x: 0,
    y: 422,
    purpose: "Signing secret versions, encrypted with AES-256-GCM. Rotation adds a version and sets retire_at on the others, so during the overlap every request carries one signature per active secret.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "endpoint_id", type: "bigint", fk: "endpoints.id" },
      { name: "version", type: "int" },
      { name: "ciphertext", type: "bytea" },
      { name: "key_id", type: "text" },
      { name: "fingerprint", type: "text" },
      { name: "created_at", type: ts },
      { name: "retire_at", type: ts, nullable: true },
    ],
  },
  {
    name: "subscriptions",
    plane: "delivery",
    x: 0,
    y: 633,
    purpose: "The event types an endpoint receives. An endpoint without rows receives every type.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "endpoint_id", type: "bigint", fk: "endpoints.id" },
      { name: "event_type", type: "text" },
      { name: "created_at", type: ts },
    ],
    indexes: [{ name: "subscriptions_event_type_idx", why: "Fan-out: which endpoints take this type." }],
  },
  {
    name: "endpoint_health",
    plane: "delivery",
    x: 600,
    y: 401,
    purpose: "The authoritative health streak of each endpoint, updated inside the attempt's own transaction. Observations from an older endpoint revision are ignored.",
    columns: [
      { name: "endpoint_id", type: "bigint", pk: true, fk: "endpoints.id" },
      { name: "workspace_id", type: ws },
      { name: "application_id", type: "bigint" },
      { name: "revision", type: "int" },
      { name: "consecutive_failures", type: "int" },
      { name: "last_state", type: "text" },
      { name: "observed_at", type: ts },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "health_episodes",
    plane: "delivery",
    x: 600,
    y: 631,
    purpose: "One row per period of failure: opened on the third consecutive failure, resolved by the next success. Notifications are keyed by the episode, never by an attempt.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "endpoint_id", type: "bigint", fk: "endpoints.id" },
      { name: "workspace_id", type: ws },
      { name: "revision", type: "int" },
      { name: "consecutive_failures", type: "int" },
      { name: "opened_at", type: ts },
      { name: "resolved_at", type: ts, nullable: true },
      { name: "created_at", type: ts },
    ],
    indexes: [{ name: "health_episodes_open_idx", why: "Unique and partial: at most one open episode per endpoint." }],
  },
  {
    name: "deliveries",
    plane: "delivery",
    x: 600,
    y: 0,
    purpose: "One delivery cycle of a message to an endpoint, and the schedule itself: due_at says when, the lease fences the attempt in progress, deadline_at ends the cycle. Replays are new rows linked to the cycle they repeat.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "message_id", type: "bigint", fk: "messages.id" },
      { name: "endpoint_id", type: "bigint", fk: "endpoints.id" },
      { name: "workspace_id", type: ws },
      { name: "endpoint_revision", type: "int" },
      { name: "state", type: "text" },
      { name: "attempt_count", type: "int" },
      { name: "due_at", type: ts, nullable: true },
      { name: "deadline_at", type: ts },
      { name: "lease_token", type: "text", nullable: true },
      { name: "lease_expires_at", type: ts, nullable: true },
      { name: "cancel_requested", type: "bool" },
      { name: "failure_reason", type: "text", nullable: true },
      { name: "replay_of_delivery_id", type: "bigint", nullable: true, fk: "deliveries.id" },
      { name: "replay_idempotency_key", type: "text", nullable: true },
      { name: "completed_at", type: ts, nullable: true },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
    indexes: [
      { name: "deliveries_due_idx", why: "Partial over queued and retry_scheduled rows: the claim reads only work that can run." },
      { name: "deliveries_lease_idx", why: "Partial over in_flight rows: the sweep finds expired leases." },
      { name: "deliveries_active_cycle_idx", why: "Unique and partial: one active cycle per message and endpoint, which is what makes a concurrent replay a 409." },
      { name: "deliveries_inflight_endpoint_idx", why: "Counts an endpoint's attempts in flight for its concurrency cap." },
      { name: "deliveries_workspace_active_idx", why: "Counts a workspace's unfinished deliveries for the backlog quota, bounded so it stops at the cap." },
      { name: "deliveries_message_state_idx", why: "Per-state counts in the event explorer." },
    ],
  },
  {
    name: "attempts",
    plane: "delivery",
    x: 900,
    y: 0,
    purpose: "One row per HTTP request that may have been sent, kept even when the result is unknown. It records exactly what was used: URL, endpoint revision and secret version.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "delivery_id", type: "bigint", fk: "deliveries.id" },
      { name: "number", type: "int" },
      { name: "endpoint_revision", type: "int" },
      { name: "secret_version", type: "int" },
      { name: "request_url", type: "text" },
      { name: "started_at", type: ts },
      { name: "completed_at", type: ts, nullable: true },
      { name: "latency_ms", type: "int" },
      { name: "status_code", type: "int", nullable: true },
      { name: "error_class", type: "text", nullable: true },
      { name: "response_headers", type: "jsonb" },
      { name: "response_preview", type: "text", nullable: true },
    ],
  },
  {
    name: "audit_events",
    plane: "delivery",
    x: 900,
    y: 306,
    purpose: "Who changed an endpoint, read a payload, cancelled or replayed, written in the same transaction as the change. Details never hold secrets or payload bytes.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "actor_id", type: ws },
      { name: "action", type: "text" },
      { name: "resource_type", type: "text" },
      { name: "resource_id", type: "bigint", nullable: true },
      { name: "request_id", type: "text" },
      { name: "details", type: "jsonb" },
      { name: "occurred_at", type: ts },
    ],
  },
  {
    name: "outbox",
    plane: "delivery",
    x: 900,
    y: 536,
    purpose: "Events written in the transaction that caused them and published afterwards by the relay: delivery.due wake-ups, delivery state changes, health changes.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "type", type: "text" },
      { name: "payload", type: "jsonb" },
      { name: "occurred_at", type: ts },
      { name: "published_at", type: ts, nullable: true },
      { name: "trace_parent", type: "text" },
    ],
    indexes: [{ name: "outbox_unpublished_idx", why: "Partial: the relay reads only what is still to publish." }],
  },
  {
    name: "processed_events",
    plane: "delivery",
    x: 900,
    y: 709,
    purpose: "Event ids already handled, for a consumer that must act once per event. The data plane's only consumer takes wake-ups, which need no entry because a duplicate only claims again, so today this table stays empty.",
    columns: [
      { name: "event_id", type: "bigint", pk: true },
      { name: "processed_at", type: ts },
    ],
  },
];

export const controlTables: Table[] = [
  {
    name: "api_keys",
    plane: "control",
    x: 0,
    y: 0,
    purpose: "Sender credentials. Only the prefix and a SHA-256 hash are stored; the raw key is shown once. A key may be bound to one application, and scopes limit what it can call.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws, ref: "goauth.organizations" },
      { name: "application_id", type: "bigint", nullable: true, ref: "delivery.applications" },
      { name: "name", type: "text" },
      { name: "prefix", type: "text" },
      { name: "key_hash", type: "text" },
      { name: "scopes", type: "text[]" },
      { name: "created_by", type: ws },
      { name: "expires_at", type: ts, nullable: true },
      { name: "last_used_at", type: ts, nullable: true },
      { name: "revoked_at", type: ts, nullable: true },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
    indexes: [{ name: "api_keys_application_idx", why: "Partial: only bound keys." }],
  },
  {
    name: "member_roles",
    plane: "control",
    x: 0,
    y: 306,
    purpose: "Refines goauth's member role into developer or viewer. goauth's owner and admin need no row.",
    columns: [
      { name: "workspace_id", type: ws, pk: true, ref: "goauth.organizations" },
      { name: "user_id", type: ws, pk: true, ref: "goauth.users" },
      { name: "role", type: "text" },
      { name: "updated_by", type: ws },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "endpoint_health",
    plane: "control",
    x: 0,
    y: 479,
    purpose: "The control plane's projection of delivery state events, one row per endpoint. Built from events, so it can lag; the data plane's own table is authoritative.",
    columns: [
      { name: "endpoint_id", type: "bigint", pk: true, ref: "delivery.endpoints" },
      { name: "workspace_id", type: ws },
      { name: "application_id", type: "bigint" },
      { name: "consecutive_failures", type: "int" },
      { name: "last_state", type: "text" },
      { name: "last_reason", type: "text", nullable: true },
      { name: "last_status_code", type: "int", nullable: true },
      { name: "last_delivery_id", type: "bigint" },
      { name: "last_attempt", type: "int" },
      { name: "observed_at", type: ts },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "notifications",
    plane: "control",
    x: 620,
    y: 0,
    purpose: "In-app notices of failure and recovery, one per episode, kind and recipient. The unique index is what makes a redelivered health event harmless.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "user_id", type: ws, ref: "goauth.users" },
      { name: "kind", type: "text" },
      { name: "episode_id", type: "bigint", ref: "delivery.health_episodes" },
      { name: "endpoint_id", type: "bigint" },
      { name: "endpoint_url", type: "text" },
      { name: "application_id", type: "bigint" },
      { name: "title", type: "text" },
      { name: "body", type: "text" },
      { name: "read_at", type: ts, nullable: true },
      { name: "created_at", type: ts },
    ],
    indexes: [{ name: "notifications_once_idx", why: "Unique (episode, kind, recipient): at most one notice each." }],
  },
  {
    name: "notification_preferences",
    plane: "control",
    x: 620,
    y: 287,
    purpose: "Each person's email alert choice. No row means alerts are on.",
    columns: [
      { name: "user_id", type: ws, pk: true, ref: "goauth.users" },
      { name: "email_alerts", type: "bool" },
      { name: "created_at", type: ts },
      { name: "updated_at", type: ts },
    ],
  },
  {
    name: "audit_events",
    plane: "control",
    x: 920,
    y: 0,
    purpose: "Key creation and revocation, role changes and guarded membership changes. The audit screen merges these rows with the data plane's, newest first.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "workspace_id", type: ws },
      { name: "actor_id", type: ws },
      { name: "action", type: "text" },
      { name: "resource_type", type: "text" },
      { name: "resource_id", type: "bigint", nullable: true },
      { name: "request_id", type: "text" },
      { name: "details", type: "jsonb" },
      { name: "occurred_at", type: ts },
    ],
  },
  {
    name: "outbox",
    plane: "control",
    x: 920,
    y: 230,
    purpose: "The control plane's own outbox: notification.email_requested, written with the notice it mails.",
    columns: [
      { name: "id", type: "bigint", pk: true },
      { name: "type", type: "text" },
      { name: "payload", type: "jsonb" },
      { name: "occurred_at", type: ts },
      { name: "published_at", type: ts, nullable: true },
      { name: "trace_parent", type: "text" },
    ],
  },
  {
    name: "processed_events",
    plane: "control",
    x: 920,
    y: 403,
    purpose: "Event ids the control plane worker has handled; its handlers run in the same transaction as the mark, so a redelivery is skipped.",
    columns: [
      { name: "event_id", type: "bigint", pk: true },
      { name: "processed_at", type: ts },
    ],
  },
];

// Tables the control plane refers to by id without owning them: goauth's in the same database, the data plane's
// in another one. They are drawn as references only; Hookbeam never writes SQL against them.
export const referencedTables: { name: string; owner: "goauth" | "delivery"; x: number; y: number; tables: string[] }[] = [
  { name: "goauth", owner: "goauth", x: 330, y: 40, tables: ["organizations", "users", "organization_members", "org_invitations", "tokens"] },
  { name: "data plane", owner: "delivery", x: 330, y: 330, tables: ["applications", "endpoints", "health_episodes"] },
];
