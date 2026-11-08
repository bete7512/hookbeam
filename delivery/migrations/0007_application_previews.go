package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// response_previews_enabled lets an application opt into exposing attempt response previews to readers of payloads.
func (s *service) registerMigration_0007_ApplicationPreviews() {
	goose.AddMigrationContext(s.up_0007_ApplicationPreviews, s.down_0007_ApplicationPreviews)
}

func (s *service) up_0007_ApplicationPreviews(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE applications ADD COLUMN response_previews_enabled BOOLEAN NOT NULL DEFAULT false`)
	return err
}

func (s *service) down_0007_ApplicationPreviews(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE applications DROP COLUMN IF EXISTS response_previews_enabled`)
	return err
}
