package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0013 stores an endpoint's custom headers. Their values are credentials for the receiver's system, so the whole
// set is one JSON object encrypted like the signing secrets (AES-GCM under headers_key_id); NULL means none.
func (s *service) registerMigration_0013_EndpointHeaders() {
	goose.AddMigrationContext(s.up_0013_EndpointHeaders, s.down_0013_EndpointHeaders)
}

func (s *service) up_0013_EndpointHeaders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE endpoints ADD COLUMN headers_ciphertext BYTEA NULL;
ALTER TABLE endpoints ADD COLUMN headers_key_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *service) down_0013_EndpointHeaders(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE endpoints DROP COLUMN headers_key_id;
ALTER TABLE endpoints DROP COLUMN headers_ciphertext`)
	return err
}
