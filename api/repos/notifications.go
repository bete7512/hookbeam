package repos

//go:generate mockgen -source=notifications.go -destination=repo_mocks/mock_notifications.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// NotificationRepo stores in-app notifications, one per episode, kind and recipient.
type NotificationRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) NotificationRepo
	// CreateNotification inserts the notice and fills its id; false means the same episode, kind and recipient
	// were already notified and nothing was written.
	CreateNotification(ctx context.Context, n *models.Notification) (bool, error)
	// GetNotificationByID returns a notice regardless of workspace; for the worker.
	GetNotificationByID(ctx context.Context, id int64) (*models.Notification, error)
	// ListNotifications pages a user's notices in the workspace, newest first.
	ListNotifications(ctx context.Context, opts ListNotificationsOpts) ([]models.Notification, error)
	// CountNotifications counts what ListNotifications would page through.
	CountNotifications(ctx context.Context, opts ListNotificationsOpts) (int, error)
	// CountUnread counts the user's unread notices in the workspace.
	CountUnread(ctx context.Context, workspaceID, userID string) (int, error)
	// MarkNotificationRead sets read_at once and returns the notice; reading it again changes nothing.
	MarkNotificationRead(ctx context.Context, workspaceID, userID string, id int64) (*models.Notification, error)
}

// ListNotificationsOpts pages a recipient's notices.
type ListNotificationsOpts struct {
	WorkspaceID string
	UserID      string
	UnreadOnly  bool
	Limit       int
	Offset      int
}

type notificationRepo struct {
	db db.DBTX
}

// NewNotificationRepo returns a NotificationRepo over a pool or a transaction.
func NewNotificationRepo(conn db.DBTX) (NotificationRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &notificationRepo{db: conn}, nil
}

func (r *notificationRepo) WithTx(tx db.DBTX) NotificationRepo { return &notificationRepo{db: tx} }

const (
	notificationColumns = `id, workspace_id, user_id, kind, episode_id, endpoint_id, endpoint_url, application_id, title, body, read_at, created_at`
	notificationFilter  = `workspace_id = @workspace_id AND user_id = @user_id AND (NOT @unread_only::bool OR read_at IS NULL)`
)

func (r *notificationRepo) CreateNotification(ctx context.Context, n *models.Notification) (bool, error) {
	query := `
	INSERT INTO notifications (workspace_id, user_id, kind, episode_id, endpoint_id, endpoint_url, application_id, title, body)
	VALUES (@workspace_id, @user_id, @kind, @episode_id, @endpoint_id, @endpoint_url, @application_id, @title, @body)
	ON CONFLICT (episode_id, kind, user_id) DO NOTHING
	RETURNING ` + notificationColumns
	args := pgx.NamedArgs{
		"workspace_id":   n.WorkspaceID,
		"user_id":        n.UserID,
		"kind":           n.Kind,
		"episode_id":     n.EpisodeID,
		"endpoint_id":    n.EndpointID,
		"endpoint_url":   n.EndpointURL,
		"application_id": n.ApplicationID,
		"title":          n.Title,
		"body":           n.Body,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return false, db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Notification])
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, db.HandleError(err)
	}
	*n = created
	return true, nil
}

func (r *notificationRepo) GetNotificationByID(ctx context.Context, id int64) (*models.Notification, error) {
	return r.one(ctx, `SELECT `+notificationColumns+` FROM notifications WHERE id = @id`, pgx.NamedArgs{"id": id})
}

func (r *notificationRepo) ListNotifications(ctx context.Context, opts ListNotificationsOpts) ([]models.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE ` + notificationFilter + ` ORDER BY created_at DESC, id DESC LIMIT @limit OFFSET @offset`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{
		"workspace_id": opts.WorkspaceID,
		"user_id":      opts.UserID,
		"unread_only":  opts.UnreadOnly,
		"limit":        opts.Limit,
		"offset":       opts.Offset,
	})
	if err != nil {
		return nil, db.HandleError(err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Notification])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return list, nil
}

func (r *notificationRepo) CountNotifications(ctx context.Context, opts ListNotificationsOpts) (int, error) {
	var total int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE `+notificationFilter, pgx.NamedArgs{
		"workspace_id": opts.WorkspaceID,
		"user_id":      opts.UserID,
		"unread_only":  opts.UnreadOnly,
	}).Scan(&total)
	return total, db.HandleError(err)
}

func (r *notificationRepo) CountUnread(ctx context.Context, workspaceID, userID string) (int, error) {
	var total int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE workspace_id = @workspace_id AND user_id = @user_id AND read_at IS NULL`, pgx.NamedArgs{
		"workspace_id": workspaceID,
		"user_id":      userID,
	}).Scan(&total)
	return total, db.HandleError(err)
}

func (r *notificationRepo) MarkNotificationRead(ctx context.Context, workspaceID, userID string, id int64) (*models.Notification, error) {
	query := `UPDATE notifications SET read_at = COALESCE(read_at, now()) WHERE workspace_id = @workspace_id AND user_id = @user_id AND id = @id RETURNING ` + notificationColumns
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "user_id": userID, "id": id})
}

func (r *notificationRepo) one(ctx context.Context, query string, args pgx.NamedArgs) (*models.Notification, error) {
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	n, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Notification])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return n, nil
}
