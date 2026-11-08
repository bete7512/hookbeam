package repos

//go:generate mockgen -source=notification_preferences.go -destination=repo_mocks/mock_notification_preferences.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// NotificationPreferenceRepo stores each user's alert email choice.
type NotificationPreferenceRepo interface {
	// GetPreferences returns the user's row, or ErrNotFound when the user kept the default.
	GetPreferences(ctx context.Context, userID string) (*models.NotificationPreferences, error)
	// SetPreferences stores or replaces the user's choice.
	SetPreferences(ctx context.Context, p *models.NotificationPreferences) error
}

type notificationPreferenceRepo struct {
	db db.DBTX
}

// NewNotificationPreferenceRepo returns a NotificationPreferenceRepo over a pool or a transaction.
func NewNotificationPreferenceRepo(conn db.DBTX) (NotificationPreferenceRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &notificationPreferenceRepo{db: conn}, nil
}

const notificationPreferenceColumns = `user_id, email_alerts, updated_at`

func (r *notificationPreferenceRepo) GetPreferences(ctx context.Context, userID string) (*models.NotificationPreferences, error) {
	rows, err := r.db.Query(ctx, `SELECT `+notificationPreferenceColumns+` FROM notification_preferences WHERE user_id = @user_id`, pgx.NamedArgs{"user_id": userID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	p, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.NotificationPreferences])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return p, nil
}

func (r *notificationPreferenceRepo) SetPreferences(ctx context.Context, p *models.NotificationPreferences) error {
	query := `
	INSERT INTO notification_preferences (user_id, email_alerts) VALUES (@user_id, @email_alerts)
	ON CONFLICT (user_id) DO UPDATE SET email_alerts = EXCLUDED.email_alerts
	RETURNING ` + notificationPreferenceColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"user_id": p.UserID, "email_alerts": p.EmailAlerts})
	if err != nil {
		return db.HandleError(err)
	}
	stored, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.NotificationPreferences])
	if err != nil {
		return db.HandleError(err)
	}
	*p = stored
	return nil
}
