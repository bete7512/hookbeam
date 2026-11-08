package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"
)

// notifications holds one in-app notice per episode, kind and recipient; the unique index is what makes a
// redelivered health event harmless. notification_preferences holds each user's email opt-out.
func (s *service) registerMigration_0007_CreateNotifications() {
	goose.AddMigrationContext(s.up_0007_CreateNotifications, s.down_0007_CreateNotifications)
}

func (s *service) up_0007_CreateNotifications(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE notifications (
  id             BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  workspace_id   VARCHAR(36) NOT NULL,
  user_id        VARCHAR(36) NOT NULL,
  kind           TEXT        NOT NULL CHECK (kind IN ('failure', 'recovery')),
  episode_id     BIGINT      NOT NULL,
  endpoint_id    BIGINT      NOT NULL,
  endpoint_url   TEXT        NOT NULL,
  application_id BIGINT      NOT NULL,
  title          TEXT        NOT NULL,
  body           TEXT        NOT NULL,
  read_at        TIMESTAMPTZ NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX notifications_once_idx ON notifications (episode_id, kind, user_id);
CREATE INDEX notifications_inbox_idx ON notifications (workspace_id, user_id, created_at DESC, id DESC);

CREATE TABLE notification_preferences (
  user_id      VARCHAR(36) PRIMARY KEY,
  email_alerts BOOLEAN     NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TRIGGER update_notification_preferences_updated_at BEFORE UPDATE ON notification_preferences FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`)
	return err
}

func (s *service) down_0007_CreateNotifications(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS notification_preferences; DROP TABLE IF EXISTS notifications`)
	return err
}
