package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0008 stores the producing transaction's trace context next to each outbox event, so the relay and the
// consumers can link their spans to it.
func (s *service) registerMigration_0008_OutboxTraceParent() {
	goose.AddMigrationContext(s.up_0008_OutboxTraceParent, s.down_0008_OutboxTraceParent)
}

func (s *service) up_0008_OutboxTraceParent(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE outbox ADD COLUMN trace_parent TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *service) down_0008_OutboxTraceParent(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `ALTER TABLE outbox DROP COLUMN trace_parent`)
	return err
}
