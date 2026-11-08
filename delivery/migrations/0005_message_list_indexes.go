package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0005 adds the indexes the message explorer filters by: event type per application and delivery state per message.
func (s *service) registerMigration_0005_MessageListIndexes() {
	goose.AddMigrationNoTxContext(s.up_0005_MessageListIndexes, s.down_0005_MessageListIndexes)
}

func (s *service) up_0005_MessageListIndexes(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS messages_application_type_idx ON messages (application_id, event_type, id DESC)`,
		`CREATE INDEX CONCURRENTLY IF NOT EXISTS deliveries_message_state_idx ON deliveries (message_id, state)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) down_0005_MessageListIndexes(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`DROP INDEX CONCURRENTLY IF EXISTS messages_application_type_idx`,
		`DROP INDEX CONCURRENTLY IF EXISTS deliveries_message_state_idx`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
