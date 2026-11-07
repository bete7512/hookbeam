package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0002_CreateSessions() {
	goose.AddMigrationContext(s.up_0002_CreateSessions, s.down_0002_CreateSessions)
}

func (s *service) up_0002_CreateSessions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE sessions (
  id                  BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id             BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  refresh_token_hash  BYTEA       NOT NULL UNIQUE,
  previous_token_hash BYTEA       NULL UNIQUE,
  expires_at          TIMESTAMPTZ NOT NULL,
  revoked_at          TIMESTAMPTZ NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
`)
	return err
}

func (s *service) down_0002_CreateSessions(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS sessions;`)
	return err
}
