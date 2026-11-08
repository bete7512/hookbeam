package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0010 adds the partial index the per-workspace backlog count reads: only non-terminal deliveries.
func (s *service) registerMigration_0010_DeliveriesBacklogIndex() {
	goose.AddMigrationNoTxContext(s.up_0010_DeliveriesBacklogIndex, s.down_0010_DeliveriesBacklogIndex)
}

func (s *service) up_0010_DeliveriesBacklogIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS deliveries_workspace_active_idx ON deliveries (workspace_id) WHERE state IN ('queued', 'in_flight', 'retry_scheduled')`)
	return err
}

func (s *service) down_0010_DeliveriesBacklogIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS deliveries_workspace_active_idx`)
	return err
}
