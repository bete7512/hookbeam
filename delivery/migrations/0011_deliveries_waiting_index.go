package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0011 adds the partial index behind an endpoint's waiting count: its queued and retry-scheduled deliveries.
func (s *service) registerMigration_0011_DeliveriesWaitingIndex() {
	goose.AddMigrationNoTxContext(s.up_0011_DeliveriesWaitingIndex, s.down_0011_DeliveriesWaitingIndex)
}

func (s *service) up_0011_DeliveriesWaitingIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS deliveries_endpoint_waiting_idx ON deliveries (endpoint_id) WHERE state IN ('queued', 'retry_scheduled')`)
	return err
}

func (s *service) down_0011_DeliveriesWaitingIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS deliveries_endpoint_waiting_idx`)
	return err
}
