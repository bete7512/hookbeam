package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// audit_events records who changed what in the data plane, written in the same transaction as the change.
// Details never hold secrets or payload bytes.
func (s *service) registerMigration_0006_AuditEvents() {
	goose.AddMigrationContext(s.up_0006_AuditEvents, s.down_0006_AuditEvents)
}

func (s *service) up_0006_AuditEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE audit_events (
  id            BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id  VARCHAR(36) NOT NULL,
  actor_id      VARCHAR(36) NOT NULL,
  action        TEXT        NOT NULL,
  resource_type TEXT        NOT NULL,
  resource_id   BIGINT      NULL,
  request_id    TEXT        NOT NULL DEFAULT '',
  details       JSONB       NOT NULL DEFAULT '{}',
  occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_workspace_idx ON audit_events (workspace_id, occurred_at DESC, id DESC);
CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at);
`)
	return err
}

func (s *service) down_0006_AuditEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS audit_events`)
	return err
}
