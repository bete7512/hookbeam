package repos

//go:generate mockgen -source=environments.go -destination=repo_mocks/mock_environments.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// EnvironmentRepo stores a workspace's environments.
type EnvironmentRepo interface {
	CreateEnvironment(ctx context.Context, e *models.Environment) error
	GetEnvironment(ctx context.Context, workspaceID string, id int64) (*models.Environment, error)
	// ListEnvironments returns the workspace's environments in id order.
	ListEnvironments(ctx context.Context, workspaceID string) ([]models.Environment, error)
	// EnsureDefaultEnvironment returns the workspace's development environment, creating "development" if none exists.
	EnsureDefaultEnvironment(ctx context.Context, workspaceID, createdBy string) (*models.Environment, error)
}

type environmentRepo struct {
	db db.DBTX
}

// NewEnvironmentRepo returns an EnvironmentRepo over a pool or a transaction.
func NewEnvironmentRepo(conn db.DBTX) (EnvironmentRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &environmentRepo{db: conn}, nil
}

const environmentColumns = `id, workspace_id, name, kind, created_by, created_at, updated_at`

func (r *environmentRepo) CreateEnvironment(ctx context.Context, e *models.Environment) error {
	query := `INSERT INTO environments (workspace_id, name, kind, created_by) VALUES (@workspace_id, @name, @kind, @created_by) RETURNING ` + environmentColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{
		"workspace_id": e.WorkspaceID,
		"name":         e.Name,
		"kind":         e.Kind,
		"created_by":   e.CreatedBy,
	})
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Environment])
	if err != nil {
		return db.HandleError(err)
	}
	*e = created
	return nil
}

func (r *environmentRepo) GetEnvironment(ctx context.Context, workspaceID string, id int64) (*models.Environment, error) {
	rows, err := r.db.Query(ctx, `SELECT `+environmentColumns+` FROM environments WHERE workspace_id = @workspace_id AND id = @id`, pgx.NamedArgs{
		"workspace_id": workspaceID,
		"id":           id,
	})
	if err != nil {
		return nil, db.HandleError(err)
	}
	e, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Environment])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return e, nil
}

func (r *environmentRepo) ListEnvironments(ctx context.Context, workspaceID string) ([]models.Environment, error) {
	rows, err := r.db.Query(ctx, `SELECT `+environmentColumns+` FROM environments WHERE workspace_id = @workspace_id ORDER BY id`, pgx.NamedArgs{"workspace_id": workspaceID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Environment])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *environmentRepo) EnsureDefaultEnvironment(ctx context.Context, workspaceID, createdBy string) (*models.Environment, error) {
	_, err := r.db.Exec(ctx, `INSERT INTO environments (workspace_id, name, kind, created_by) VALUES (@workspace_id, 'development', 'development', @created_by) ON CONFLICT (workspace_id, name) DO NOTHING`, pgx.NamedArgs{
		"workspace_id": workspaceID,
		"created_by":   createdBy,
	})
	if err != nil {
		return nil, db.HandleError(err)
	}
	rows, err := r.db.Query(ctx, `SELECT `+environmentColumns+` FROM environments WHERE workspace_id = @workspace_id AND kind = 'development' ORDER BY id LIMIT 1`, pgx.NamedArgs{"workspace_id": workspaceID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	e, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Environment])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return e, nil
}
