package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// member_roles refines goauth's "member" into Hookbeam's developer or viewer; owners and admins are goauth's
// roles as they are. Ids are goauth's VARCHAR(36), with no foreign key into its tables.
func (s *service) registerMigration_0004_CreateMemberRoles() {
	goose.AddMigrationContext(s.up_0004_CreateMemberRoles, s.down_0004_CreateMemberRoles)
}

func (s *service) up_0004_CreateMemberRoles(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE member_roles (
  workspace_id VARCHAR(36) NOT NULL,
  user_id      VARCHAR(36) NOT NULL,
  role         TEXT        NOT NULL CHECK (role IN ('developer', 'viewer')),
  updated_by   VARCHAR(36) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, user_id)
);

CREATE TRIGGER update_member_roles_updated_at BEFORE UPDATE ON member_roles FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`)
	return err
}

func (s *service) down_0004_CreateMemberRoles(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS member_roles`)
	return err
}
