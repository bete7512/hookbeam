package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// deliveries.workspace_id denormalizes the message's workspace so the backlog can be counted per workspace
// without a join. The default keeps inserts from an older binary valid during a rolling deploy.
func (s *service) registerMigration_0009_DeliveriesWorkspace() {
	goose.AddMigrationContext(s.up_0009_DeliveriesWorkspace, s.down_0009_DeliveriesWorkspace)
}

func (s *service) up_0009_DeliveriesWorkspace(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE deliveries ADD COLUMN workspace_id VARCHAR(36) NOT NULL DEFAULT '';
UPDATE deliveries d SET workspace_id = m.workspace_id FROM messages m WHERE m.id = d.message_id AND d.workspace_id = '';
`)
	return err
}

func (s *service) down_0009_DeliveriesWorkspace(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE deliveries DROP COLUMN IF EXISTS workspace_id`)
	return err
}
