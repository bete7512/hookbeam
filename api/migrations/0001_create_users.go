package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0001_CreateUsers() {
	goose.AddMigrationContext(s.up_0001_CreateUsers, s.down_0001_CreateUsers)
}

func (s *service) up_0001_CreateUsers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE EXTENSION IF NOT EXISTS citext;

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TABLE IF NOT EXISTS users (
  id                BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email             CITEXT      UNIQUE NOT NULL,
  name              TEXT        NOT NULL DEFAULT '',
  password_hash     TEXT        NULL,
  email_verified_at TIMESTAMPTZ NULL,
  token_version     INTEGER     NOT NULL DEFAULT 0,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
	`)
	return err
}

// The citext extension is left in place: dropping an extension is a
// schema-wide decision, not this migration's.
func (s *service) down_0001_CreateUsers(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS update_users_updated_at ON users;

DROP TABLE IF EXISTS users;

DROP FUNCTION IF EXISTS update_updated_at_column;
	`)
	return err
}
