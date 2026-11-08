// Package models holds the data plane's row types.
package models

import "time"

// Application is one recipient boundary inside a workspace.
type Application struct {
	ID              int64  `db:"id"`
	WorkspaceID     string `db:"workspace_id"` // goauth organization id
	EnvironmentID   int64  `db:"environment_id"`
	EnvironmentKind string `db:"environment_kind"` // joined from environments on reads
	Name            string `db:"name"`
	CreatedBy       string `db:"created_by"` // goauth user id
	// ResponsePreviewsEnabled exposes attempt response previews to readers allowed to see payloads.
	ResponsePreviewsEnabled bool       `db:"response_previews_enabled"`
	ArchivedAt              *time.Time `db:"archived_at"`
	CreatedAt               time.Time  `db:"created_at"`
	UpdatedAt               time.Time  `db:"updated_at"`
}
