package repos

//go:generate mockgen -source=endpoints.go -destination=repo_mocks/mock_endpoints.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// EndpointRepo reads and writes endpoints; every read is scoped to a workspace. EventTypes are the subscription repo's.
type EndpointRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) EndpointRepo
	CreateEndpoint(ctx context.Context, e *models.Endpoint) error
	GetEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error)
	// PauseEndpoint sets paused_at on an active, unpaused endpoint and returns it; ErrNotFound when no such row.
	PauseEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error)
}

type endpointRepo struct {
	db db.DBTX
}

// NewEndpointRepo returns an EndpointRepo over a pool or a transaction.
func NewEndpointRepo(conn db.DBTX) (EndpointRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &endpointRepo{db: conn}, nil
}

func (r *endpointRepo) WithTx(tx db.DBTX) EndpointRepo { return &endpointRepo{db: tx} }

const endpointColumns = `id, workspace_id, application_id, url, description, revision, paused_at, archived_at, created_by, created_at, updated_at`

func (r *endpointRepo) CreateEndpoint(ctx context.Context, e *models.Endpoint) error {
	query := `
	INSERT INTO endpoints (workspace_id, application_id, url, description, created_by)
	VALUES (@workspace_id, @application_id, @url, @description, @created_by)
	RETURNING ` + endpointColumns
	args := pgx.NamedArgs{
		"workspace_id":   e.WorkspaceID,
		"application_id": e.ApplicationID,
		"url":            e.URL,
		"description":    e.Description,
		"created_by":     e.CreatedBy,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Endpoint])
	if err != nil {
		return db.HandleError(err)
	}
	created.EventTypes = e.EventTypes
	*e = created
	return nil
}

func (r *endpointRepo) GetEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error) {
	query := `SELECT ` + endpointColumns + ` FROM endpoints WHERE workspace_id = @workspace_id AND id = @id`
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
}

func (r *endpointRepo) PauseEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error) {
	query := `
	UPDATE endpoints SET paused_at = now()
	WHERE workspace_id = @workspace_id AND id = @id AND paused_at IS NULL AND archived_at IS NULL
	RETURNING ` + endpointColumns
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
}

func (r *endpointRepo) one(ctx context.Context, query string, args pgx.NamedArgs) (*models.Endpoint, error) {
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	e, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Endpoint])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return e, nil
}
