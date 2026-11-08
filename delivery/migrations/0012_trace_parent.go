package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0012 stores trace context: on outbox events for the relay and the consumers to link to, and on messages so
// every attempt links back to the trace that accepted the message.
func (s *service) registerMigration_0012_TraceParent() {
	goose.AddMigrationContext(s.up_0012_TraceParent, s.down_0012_TraceParent)
}

func (s *service) up_0012_TraceParent(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE outbox ADD COLUMN trace_parent TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN trace_parent TEXT NOT NULL DEFAULT ''`)
	return err
}

func (s *service) down_0012_TraceParent(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
ALTER TABLE messages DROP COLUMN trace_parent;
ALTER TABLE outbox DROP COLUMN trace_parent`)
	return err
}
