package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0006_CreateUserIdentities() {
	goose.AddMigrationContext(s.up_0006_CreateUserIdentities, s.down_0006_CreateUserIdentities)
}

func (s *service) up_0006_CreateUserIdentities(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE user_identities (
  id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  provider   TEXT        NOT NULL,
  subject    TEXT        NOT NULL,
  email      TEXT        NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, subject)
);

CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);
`)
	return err
}

func (s *service) down_0006_CreateUserIdentities(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS user_identities;`)
	return err
}
