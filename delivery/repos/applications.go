// Package repos is the only place the data plane runs SQL.
package repos

//go:generate mockgen -source=applications.go -destination=repo_mocks/mock_applications.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// ApplicationRepo reads and writes applications; every read is scoped to a workspace.
type ApplicationRepo interface {
	CreateApplication(ctx context.Context, a *models.Application) error
	GetApplication(ctx context.Context, workspaceID string, id int64) (*models.Application, error)
}

type applicationRepo struct {
	db db.DBTX
}

// NewApplicationRepo returns an ApplicationRepo over a pool or a transaction.
func NewApplicationRepo(conn db.DBTX) (ApplicationRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &applicationRepo{db: conn}, nil
}

const (
	// applicationColumns is what an insert returns; the environment's kind comes from a scalar subquery.
	applicationColumns = `id, workspace_id, environment_id, (SELECT kind FROM environments WHERE environments.id = applications.environment_id) AS environment_kind, name, created_by, archived_at, created_at, updated_at`
	// applicationReadColumns is the same shape over the join reads use.
	applicationReadColumns = `a.id, a.workspace_id, a.environment_id, e.kind AS environment_kind, a.name, a.created_by, a.archived_at, a.created_at, a.updated_at`
)

func (r *applicationRepo) CreateApplication(ctx context.Context, a *models.Application) error {
	query := `
	INSERT INTO applications (workspace_id, environment_id, name, created_by)
	VALUES (@workspace_id, @environment_id, @name, @created_by)
	RETURNING ` + applicationColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": a.WorkspaceID, "environment_id": a.EnvironmentID, "name": a.Name, "created_by": a.CreatedBy})
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Application])
	if err != nil {
		return db.HandleError(err)
	}
	*a = created
	return nil
}

func (r *applicationRepo) GetApplication(ctx context.Context, workspaceID string, id int64) (*models.Application, error) {
	query := `SELECT ` + applicationReadColumns + ` FROM applications a JOIN environments e ON e.id = a.environment_id WHERE a.workspace_id = @workspace_id AND a.id = @id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
	if err != nil {
		return nil, db.HandleError(err)
	}
	a, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Application])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return a, nil
}
