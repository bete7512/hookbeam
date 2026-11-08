package models

import "time"

// Notification kinds.
const (
	NotificationFailure  = "failure"  // an endpoint's health episode opened
	NotificationRecovery = "recovery" // it resolved
)

// Notification is one in-app notice for one recipient about one health episode.
type Notification struct {
	ID            int64      `db:"id"`
	WorkspaceID   string     `db:"workspace_id"`
	UserID        string     `db:"user_id"` // goauth user id of the recipient
	Kind          string     `db:"kind"`
	EpisodeID     int64      `db:"episode_id"`
	EndpointID    int64      `db:"endpoint_id"`
	EndpointURL   string     `db:"endpoint_url"`
	ApplicationID int64      `db:"application_id"`
	Title         string     `db:"title"`
	Body          string     `db:"body"`
	ReadAt        *time.Time `db:"read_at"`
	CreatedAt     time.Time  `db:"created_at"`
}

// NotificationPreferences is a user's choice about alert emails; a user without a row gets emails.
type NotificationPreferences struct {
	UserID      string    `db:"user_id"`
	EmailAlerts bool      `db:"email_alerts"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// UserContact is what the control plane needs from goauth to email a user.
type UserContact struct {
	Email         string
	EmailVerified bool
	Name          string
}
