package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0007_BindAuthRequests() {
	goose.AddMigrationContext(s.up_0007_BindAuthRequests, s.down_0007_BindAuthRequests)
}

func (s *service) up_0007_BindAuthRequests(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE sessions ADD COLUMN token_version INTEGER NOT NULL DEFAULT -1;
ALTER TABLE auth_tokens ADD COLUMN request_id BIGINT NULL CHECK (request_id > 0);
`)
	return err
}

func (s *service) down_0007_BindAuthRequests(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE auth_tokens DROP COLUMN request_id;
ALTER TABLE sessions DROP COLUMN token_version;
`)
	return err
}
