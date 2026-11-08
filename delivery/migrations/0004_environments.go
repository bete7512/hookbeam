package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// Environments sit between a workspace and its applications. Every workspace that already had applications
// gets one "development" environment and its applications move under it; application names become unique
// per environment instead of per workspace.
func (s *service) registerMigration_0004_Environments() {
	goose.AddMigrationContext(s.up_0004_Environments, s.down_0004_Environments)
}

func (s *service) up_0004_Environments(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE environments (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id VARCHAR(36) NOT NULL,
  name         TEXT        NOT NULL,
  kind         TEXT        NOT NULL CHECK (kind IN ('development', 'production')),
  created_by   VARCHAR(36) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, name)
);
CREATE INDEX environments_workspace_idx ON environments (workspace_id, id);
CREATE TRIGGER update_environments_updated_at BEFORE UPDATE ON environments FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();

ALTER TABLE applications ADD COLUMN environment_id BIGINT NULL REFERENCES environments (id);

INSERT INTO environments (workspace_id, name, kind, created_by)
SELECT workspace_id, 'development', 'development', min(created_by) FROM applications GROUP BY workspace_id;

UPDATE applications a SET environment_id = e.id
FROM environments e WHERE e.workspace_id = a.workspace_id AND e.kind = 'development' AND a.environment_id IS NULL;

ALTER TABLE applications ALTER COLUMN environment_id SET NOT NULL;
ALTER TABLE applications DROP CONSTRAINT applications_workspace_id_name_key;
ALTER TABLE applications ADD CONSTRAINT applications_environment_id_name_key UNIQUE (environment_id, name);
CREATE INDEX applications_environment_idx ON applications (environment_id, id);
`)
	return err
}

func (s *service) down_0004_Environments(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE applications DROP CONSTRAINT IF EXISTS applications_environment_id_name_key;
ALTER TABLE applications ADD CONSTRAINT applications_workspace_id_name_key UNIQUE (workspace_id, name);
ALTER TABLE applications DROP COLUMN IF EXISTS environment_id;
DROP TABLE IF EXISTS environments;
`)
	return err
}
