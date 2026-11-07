package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// A key may be bound to one consumer application (the data plane's id, no foreign key): it then submits
// only to that application. NULL keeps a key workspace-wide.
func (s *service) registerMigration_0005_APIKeysApplication() {
	goose.AddMigrationContext(s.up_0005_APIKeysApplication, s.down_0005_APIKeysApplication)
}

func (s *service) up_0005_APIKeysApplication(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE api_keys ADD COLUMN application_id BIGINT NULL;
CREATE INDEX api_keys_application_idx ON api_keys (workspace_id, application_id) WHERE application_id IS NOT NULL;
`)
	return err
}

func (s *service) down_0005_APIKeysApplication(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE api_keys DROP COLUMN IF EXISTS application_id`)
	return err
}
