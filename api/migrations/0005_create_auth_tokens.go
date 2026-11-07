package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

func (s *service) registerMigration_0005_CreateAuthTokens() {
	goose.AddMigrationContext(s.up_0005_CreateAuthTokens, s.down_0005_CreateAuthTokens)
}

func (s *service) up_0005_CreateAuthTokens(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE auth_tokens (
  id         BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id    BIGINT      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  purpose    TEXT        NOT NULL,
  token_hash BYTEA       NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at    TIMESTAMPTZ NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX auth_tokens_user_id_purpose_idx ON auth_tokens (user_id, purpose);
`)
	return err
}

func (s *service) down_0005_CreateAuthTokens(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS auth_tokens;`)
	return err
}
