package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// Identity tables (users, sessions, tokens, accounts, organizations) belong to
// goauth and are created by its own migrations from `api -m migrate up`.
func (s *service) registerMigration_0001_Baseline() {
	goose.AddMigrationContext(s.up_0001_Baseline, s.down_0001_Baseline)
}

func (s *service) up_0001_Baseline(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE EXTENSION IF NOT EXISTS citext;

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TABLE outbox (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  type         TEXT        NOT NULL,
  payload      JSONB       NOT NULL,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ NULL
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
CREATE INDEX outbox_published_at_idx ON outbox (published_at) WHERE published_at IS NOT NULL;

CREATE TABLE processed_events (
  event_id     BIGINT      PRIMARY KEY,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

// The citext extension is left in place: dropping an extension is a
// schema-wide decision, not this migration's.
func (s *service) down_0001_Baseline(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
DROP TABLE IF EXISTS processed_events;
DROP TABLE IF EXISTS outbox;
DROP FUNCTION IF EXISTS update_updated_at_column;
`)
	return err
}
