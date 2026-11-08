package models

import "time"

// Environment kinds, as the specification names them.
const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
)

// Environment groups a workspace's applications; development and production never share one.
type Environment struct {
	ID          int64     `db:"id"`
	WorkspaceID string    `db:"workspace_id"`
	Name        string    `db:"name"`
	Kind        string    `db:"kind"`
	CreatedBy   string    `db:"created_by"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}
