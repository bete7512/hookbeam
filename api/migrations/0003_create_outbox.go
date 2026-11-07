package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0003_CreateOutbox() {
	goose.AddMigrationContext(s.up_0003_CreateOutbox, s.down_0003_CreateOutbox)
}

func (s *service) up_0003_CreateOutbox(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE outbox (
  id           BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  type         TEXT        NOT NULL,
  payload      JSONB       NOT NULL,
  occurred_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ NULL
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
CREATE INDEX outbox_published_at_idx ON outbox (published_at) WHERE published_at IS NOT NULL;
`)
	return err
}

func (s *service) down_0003_CreateOutbox(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS outbox;`)
	return err
}
