package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// workspace_id and created_by hold goauth ids (VARCHAR(36)) with no foreign key: identity lives in the
// control plane's database. Delivery states and policy follow the product specification; ADR 0021 explains
// why Postgres, not the queue, is the schedule authority (due_at, leases, reconciliation).
func (s *service) registerMigration_0001_Baseline() {
	goose.AddMigrationContext(s.up_0001_Baseline, s.down_0001_Baseline)
}

func (s *service) up_0001_Baseline(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- A consumer application is one recipient boundary inside a workspace; no event crosses applications.
CREATE TABLE applications (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id VARCHAR(36) NOT NULL,
  name         TEXT        NOT NULL,
  created_by   VARCHAR(36) NOT NULL,
  archived_at  TIMESTAMPTZ NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, name)
);
CREATE INDEX applications_workspace_idx ON applications (workspace_id, id);
CREATE TRIGGER update_applications_updated_at BEFORE UPDATE ON applications FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();

-- revision increments on every configuration change (url, subscriptions); deliveries record the revision they were planned under.
CREATE TABLE endpoints (
  id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id   VARCHAR(36) NOT NULL,
  application_id BIGINT      NOT NULL REFERENCES applications (id),
  url            TEXT        NOT NULL,
  description    TEXT        NOT NULL DEFAULT '',
  revision       INTEGER     NOT NULL DEFAULT 1,
  paused_at      TIMESTAMPTZ NULL,
  archived_at    TIMESTAMPTZ NULL,
  created_by     VARCHAR(36) NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX endpoints_application_idx ON endpoints (application_id, id);
CREATE INDEX endpoints_workspace_idx ON endpoints (workspace_id, id);
CREATE TRIGGER update_endpoints_updated_at BEFORE UPDATE ON endpoints FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();

-- Signing secrets are encrypted at rest (AES-GCM under key_id); retire_at gives rotation its overlap window.
CREATE TABLE endpoint_secrets (
  id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  endpoint_id BIGINT      NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
  version     INTEGER     NOT NULL,
  ciphertext  BYTEA       NOT NULL,
  key_id      TEXT        NOT NULL,
  fingerprint TEXT        NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  retire_at   TIMESTAMPTZ NULL,
  UNIQUE (endpoint_id, version)
);

-- One row per event type an endpoint receives; an endpoint with no rows receives every type.
CREATE TABLE subscriptions (
  id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  endpoint_id BIGINT      NOT NULL REFERENCES endpoints (id) ON DELETE CASCADE,
  event_type  TEXT        NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (endpoint_id, event_type)
);
CREATE INDEX subscriptions_event_type_idx ON subscriptions (event_type, endpoint_id);

-- public_id is the webhook-id receivers see, independent of the bigint key; payload is BYTEA, not JSONB, because the
-- exact submitted bytes are the outbound body and what the signature covers (JSONB would reorder keys and strip spaces).
-- idempotency_key is cleared when its retention ends so the key can be reused; payload_sha256 detects a reuse with other bytes.
CREATE TABLE messages (
  id                     BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  public_id              TEXT        NOT NULL UNIQUE,
  workspace_id           VARCHAR(36) NOT NULL,
  application_id         BIGINT      NOT NULL REFERENCES applications (id),
  event_type             TEXT        NOT NULL,
  sender_event_id        TEXT        NULL,
  idempotency_key        TEXT        NULL,
  idempotency_expires_at TIMESTAMPTZ NULL,
  payload                BYTEA       NOT NULL,
  payload_sha256         BYTEA       NOT NULL,
  payload_expires_at     TIMESTAMPTZ NOT NULL,
  accepted_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX messages_idempotency_idx ON messages (application_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX messages_application_accepted_idx ON messages (application_id, accepted_at DESC, id DESC);
CREATE INDEX messages_sender_event_idx ON messages (application_id, sender_event_id) WHERE sender_event_id IS NOT NULL;
CREATE INDEX messages_payload_expiry_idx ON messages (payload_expires_at);

-- One delivery per message and subscribed endpoint; the schedule authority. due_at drives claims, the lease fences a claimed
-- attempt, deadline_at ends the cycle. One active cycle per message and endpoint; a replay links to the cycle it repeats.
CREATE TABLE deliveries (
  id                    BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  message_id            BIGINT      NOT NULL REFERENCES messages (id),
  endpoint_id           BIGINT      NOT NULL REFERENCES endpoints (id),
  endpoint_revision     INTEGER     NOT NULL,
  state                 TEXT        NOT NULL CHECK (state IN ('queued', 'in_flight', 'retry_scheduled', 'succeeded', 'failed', 'cancelled')),
  attempt_count         INTEGER     NOT NULL DEFAULT 0,
  due_at                TIMESTAMPTZ NULL,
  deadline_at           TIMESTAMPTZ NOT NULL,
  lease_token           TEXT        NULL,
  lease_expires_at      TIMESTAMPTZ NULL,
  cancel_requested      BOOLEAN     NOT NULL DEFAULT false,
  failure_reason        TEXT        NULL,
  replay_of_delivery_id BIGINT      NULL REFERENCES deliveries (id),
  replay_idempotency_key TEXT       NULL,
  completed_at          TIMESTAMPTZ NULL,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX deliveries_due_idx ON deliveries (due_at, id) WHERE state IN ('queued', 'retry_scheduled');
CREATE INDEX deliveries_lease_idx ON deliveries (lease_expires_at) WHERE state = 'in_flight';
CREATE UNIQUE INDEX deliveries_active_cycle_idx ON deliveries (message_id, endpoint_id) WHERE state IN ('queued', 'in_flight', 'retry_scheduled');
CREATE UNIQUE INDEX deliveries_replay_key_idx ON deliveries (replay_of_delivery_id, replay_idempotency_key) WHERE replay_idempotency_key IS NOT NULL;
CREATE INDEX deliveries_message_idx ON deliveries (message_id, id);
CREATE INDEX deliveries_endpoint_idx ON deliveries (endpoint_id, id);
CREATE TRIGGER update_deliveries_updated_at BEFORE UPDATE ON deliveries FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();

-- One row per network attempt, kept even when the result is unknown; response_preview is bounded by the worker.
CREATE TABLE attempts (
  id                BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  delivery_id       BIGINT      NOT NULL REFERENCES deliveries (id),
  number            INTEGER     NOT NULL,
  endpoint_revision INTEGER     NOT NULL,
  secret_version    INTEGER     NOT NULL,
  request_url       TEXT        NOT NULL,
  started_at        TIMESTAMPTZ NOT NULL,
  completed_at      TIMESTAMPTZ NULL,
  latency_ms        INTEGER     NOT NULL DEFAULT 0,
  status_code       INTEGER     NULL,
  error_class       TEXT        NULL,
  response_headers  JSONB       NOT NULL DEFAULT '{}',
  response_preview  TEXT        NULL,
  UNIQUE (delivery_id, number)
);

CREATE TABLE outbox (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  type         TEXT        NOT NULL,
  payload      JSONB       NOT NULL,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ NULL
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
CREATE INDEX outbox_published_at_idx ON outbox (published_at) WHERE published_at IS NOT NULL;

CREATE TABLE processed_events (
  event_id     BIGINT      PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

func (s *service) down_0001_Baseline(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
DROP TABLE IF EXISTS processed_events;
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS attempts;
DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS endpoint_secrets;
DROP TABLE IF EXISTS endpoints;
DROP TABLE IF EXISTS applications;
DROP FUNCTION IF EXISTS update_updated_at_column;
`)
	return err
}
