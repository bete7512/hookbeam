package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0008_ActionTokenSlot() {
	goose.AddMigrationNoTxContext(s.up_0008_ActionTokenSlot, s.down_0008_ActionTokenSlot)
}

func (s *service) up_0008_ActionTokenSlot(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS auth_tokens_active_slot_idx ON auth_tokens (user_id, purpose) WHERE request_id IS NOT NULL`)
	return err
}

func (s *service) down_0008_ActionTokenSlot(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS auth_tokens_active_slot_idx`)
	return err
}
