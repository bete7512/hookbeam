package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// workspace_id and created_by hold goauth ids (VARCHAR(36)); no foreign key into
// goauth's tables, which it migrates and may reshape on its own.
func (s *service) registerMigration_0002_CreateAPIKeys() {
	goose.AddMigrationContext(s.up_0002_CreateAPIKeys, s.down_0002_CreateAPIKeys)
}

func (s *service) up_0002_CreateAPIKeys(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE api_keys (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id VARCHAR(36) NOT NULL,
  name         TEXT        NOT NULL,
  prefix       TEXT        NOT NULL,
  key_hash     TEXT        NOT NULL UNIQUE,
  scopes       TEXT[]      NOT NULL DEFAULT '{}',
  created_by   VARCHAR(36) NOT NULL,
  expires_at   TIMESTAMPTZ NULL,
  last_used_at TIMESTAMPTZ NULL,
  revoked_at   TIMESTAMPTZ NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_workspace_idx ON api_keys (workspace_id, id);
CREATE INDEX api_keys_workspace_name_idx ON api_keys (workspace_id, name);

CREATE TRIGGER update_api_keys_updated_at BEFORE UPDATE ON api_keys FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`)
	return err
}

func (s *service) down_0002_CreateAPIKeys(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS api_keys;`)
	return err
}
