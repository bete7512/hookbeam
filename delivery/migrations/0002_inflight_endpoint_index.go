package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// 0002 adds the index the fair claim uses to count a destination's in-flight attempts.
func (s *service) registerMigration_0002_InflightEndpointIndex() {
	goose.AddMigrationNoTxContext(s.up_0002_InflightEndpointIndex, s.down_0002_InflightEndpointIndex)
}

func (s *service) up_0002_InflightEndpointIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE INDEX CONCURRENTLY IF NOT EXISTS deliveries_inflight_endpoint_idx ON deliveries (endpoint_id) WHERE state = 'in_flight'`)
	return err
}

func (s *service) down_0002_InflightEndpointIndex(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX CONCURRENTLY IF EXISTS deliveries_inflight_endpoint_idx`)
	return err
}
