package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0003 adds the indexes the retention jobs scan: payloads and idempotency keys due to expire.
func (s *service) registerMigration_0003_ExpiryIndexes() {
	goose.AddMigrationNoTxContext(s.up_0003_ExpiryIndexes, s.down_0003_ExpiryIndexes)
}

func (s *service) up_0003_ExpiryIndexes(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS messages_payload_expiry_idx ON messages (payload_expires_at) WHERE octet_length(payload) > 0`,
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS messages_key_expiry_idx ON messages (idempotency_expires_at) WHERE idempotency_key IS NOT NULL`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) down_0003_ExpiryIndexes(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`DROP INDEX CONCURRENTLY IF EXISTS messages_payload_expiry_idx`,
		`DROP INDEX CONCURRENTLY IF EXISTS messages_key_expiry_idx`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
