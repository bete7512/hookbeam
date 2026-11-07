package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0004_CreateProcessedEvents() {
	goose.AddMigrationContext(s.up_0004_CreateProcessedEvents, s.down_0004_CreateProcessedEvents)
}

func (s *service) up_0004_CreateProcessedEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE processed_events (
  event_id     BIGINT      PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

func (s *service) down_0004_CreateProcessedEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS processed_events;`)
	return err
}
