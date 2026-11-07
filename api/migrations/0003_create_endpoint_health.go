package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// endpoint_health is the control plane's projection of the data plane's delivery state events:
// one row per endpoint, updated only by newer observations. endpoint_id is the data plane's id, no foreign key.
func (s *service) registerMigration_0003_CreateEndpointHealth() {
	goose.AddMigrationContext(s.up_0003_CreateEndpointHealth, s.down_0003_CreateEndpointHealth)
}

func (s *service) up_0003_CreateEndpointHealth(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE endpoint_health (
  endpoint_id          BIGINT      PRIMARY KEY,
  workspace_id         VARCHAR(36) NOT NULL,
  application_id       BIGINT      NOT NULL,
  consecutive_failures INTEGER     NOT NULL DEFAULT 0,
  last_state           TEXT        NOT NULL,
  last_reason          TEXT        NULL,
  last_status_code     INTEGER     NULL,
  last_delivery_id     BIGINT      NOT NULL,
  last_attempt         INTEGER     NOT NULL,
  observed_at          TIMESTAMPTZ NOT NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX endpoint_health_workspace_idx ON endpoint_health (workspace_id, endpoint_id);

CREATE TRIGGER update_endpoint_health_updated_at BEFORE UPDATE ON endpoint_health FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`)
	return err
}

func (s *service) down_0003_CreateEndpointHealth(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS endpoint_health`)
	return err
}
