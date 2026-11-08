package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// audit_events records the control plane's own changes (keys, member roles, guarded organization actions).
// The data plane keeps its own table; GET /v1/audit merges both.
func (s *service) registerMigration_0006_CreateAuditEvents() {
	goose.AddMigrationContext(s.up_0006_CreateAuditEvents, s.down_0006_CreateAuditEvents)
}

func (s *service) up_0006_CreateAuditEvents(ctx context.Context, tx *sql.Tx) error {
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

func (s *service) down_0006_CreateAuditEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS audit_events`)
	return err
}
