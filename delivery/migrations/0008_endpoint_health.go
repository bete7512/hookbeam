package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// endpoint_health keeps one failure streak per endpoint, updated in the outcome transaction and guarded by the
// endpoint revision; health_episodes records every stretch of consecutive failures, at most one open per endpoint.
func (s *service) registerMigration_0008_EndpointHealth() {
	goose.AddMigrationContext(s.up_0008_EndpointHealth, s.down_0008_EndpointHealth)
}

func (s *service) up_0008_EndpointHealth(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE endpoint_health (
  endpoint_id          BIGINT      PRIMARY KEY REFERENCES endpoints (id),
  workspace_id         VARCHAR(36) NOT NULL,
  application_id       BIGINT      NOT NULL,
  revision             INTEGER     NOT NULL,
  consecutive_failures INTEGER     NOT NULL DEFAULT 0,
  last_state           TEXT        NOT NULL,
  observed_at          TIMESTAMPTZ NOT NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER update_endpoint_health_updated_at BEFORE UPDATE ON endpoint_health FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();

CREATE TABLE health_episodes (
  id                   BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  endpoint_id          BIGINT      NOT NULL REFERENCES endpoints (id),
  workspace_id         VARCHAR(36) NOT NULL,
  revision             INTEGER     NOT NULL,
  consecutive_failures INTEGER     NOT NULL,
  opened_at            TIMESTAMPTZ NOT NULL,
  resolved_at          TIMESTAMPTZ NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX health_episodes_open_idx ON health_episodes (endpoint_id) WHERE resolved_at IS NULL;
CREATE INDEX health_episodes_endpoint_idx ON health_episodes (endpoint_id, opened_at DESC);
`)
	return err
}

func (s *service) down_0008_EndpointHealth(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS health_episodes; DROP TABLE IF EXISTS endpoint_health`)
	return err
}
