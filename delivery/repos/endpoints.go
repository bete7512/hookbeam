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
	// UpdateEndpoint sets the url and description of an unarchived endpoint, bumps its revision and returns it;
	// ErrNotFound when no such row, or when expectedRevision is set and the row moved on.
	UpdateEndpoint(ctx context.Context, workspaceID string, id int64, url, description string, expectedRevision *int32) (*models.Endpoint, error)
	// ListEndpoints returns the application's endpoints in id order, archived ones included.
	ListEndpoints(ctx context.Context, workspaceID string, applicationID int64) ([]models.Endpoint, error)
	// ResumeEndpoint clears paused_at on a paused, unarchived endpoint and returns it; ErrNotFound when no such row.
	ResumeEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error)
	// ArchiveEndpoint sets archived_at on an unarchived endpoint and returns it; ErrNotFound when no such row.
	ArchiveEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error)
	// GetEndpointByID returns an endpoint without a workspace check; for the worker.
	GetEndpointByID(ctx context.Context, id int64) (*models.Endpoint, error)
	// ListSubscribedEndpoints returns the application's unarchived endpoints that receive eventType:
	// those subscribed to it and those with no subscriptions at all. Paused endpoints are included.
	ListSubscribedEndpoints(ctx context.Context, applicationID int64, eventType string) ([]models.Endpoint, error)
	// CountActiveEndpoints counts the application's unarchived endpoints.
	CountActiveEndpoints(ctx context.Context, applicationID int64) (int, error)
	// ListEndpointCounts returns every application of the workspace with its unarchived endpoint count.
	ListEndpointCounts(ctx context.Context, workspaceID string) ([]models.ApplicationEndpointCount, error)
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

func (r *endpointRepo) GetEndpointByID(ctx context.Context, id int64) (*models.Endpoint, error) {
	return r.one(ctx, `SELECT `+endpointColumns+` FROM endpoints WHERE id = @id`, pgx.NamedArgs{"id": id})
}

func (r *endpointRepo) ListSubscribedEndpoints(ctx context.Context, applicationID int64, eventType string) ([]models.Endpoint, error) {
	query := `
	SELECT ` + endpointColumns + ` FROM endpoints e
	WHERE e.application_id = @application_id AND e.archived_at IS NULL
	  AND (EXISTS (SELECT 1 FROM subscriptions s WHERE s.endpoint_id = e.id AND s.event_type = @event_type)
	       OR NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.endpoint_id = e.id))
	ORDER BY e.id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"application_id": applicationID, "event_type": eventType})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Endpoint])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *endpointRepo) PauseEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error) {
	query := `
	UPDATE endpoints SET paused_at = now()
	WHERE workspace_id = @workspace_id AND id = @id AND paused_at IS NULL AND archived_at IS NULL
	RETURNING ` + endpointColumns
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
}

func (r *endpointRepo) UpdateEndpoint(ctx context.Context, workspaceID string, id int64, url, description string, expectedRevision *int32) (*models.Endpoint, error) {
	query := `
	UPDATE endpoints SET url = @url, description = @description, revision = revision + 1
	WHERE workspace_id = @workspace_id AND id = @id AND archived_at IS NULL AND (@expected::int IS NULL OR revision = @expected::int)
	RETURNING ` + endpointColumns
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id, "url": url, "description": description, "expected": expectedRevision})
}

func (r *endpointRepo) ListEndpoints(ctx context.Context, workspaceID string, applicationID int64) ([]models.Endpoint, error) {
	query := `SELECT ` + endpointColumns + ` FROM endpoints WHERE workspace_id = @workspace_id AND application_id = @application_id ORDER BY id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "application_id": applicationID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Endpoint])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *endpointRepo) ResumeEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error) {
	query := `
	UPDATE endpoints SET paused_at = NULL
	WHERE workspace_id = @workspace_id AND id = @id AND paused_at IS NOT NULL AND archived_at IS NULL
	RETURNING ` + endpointColumns
	return r.one(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
}

func (r *endpointRepo) ArchiveEndpoint(ctx context.Context, workspaceID string, id int64) (*models.Endpoint, error) {
	query := `
	UPDATE endpoints SET archived_at = now()
	WHERE workspace_id = @workspace_id AND id = @id AND archived_at IS NULL
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

func (r *endpointRepo) CountActiveEndpoints(ctx context.Context, applicationID int64) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM endpoints WHERE application_id = @application_id AND archived_at IS NULL`, pgx.NamedArgs{"application_id": applicationID}).Scan(&n)
	return n, db.HandleError(err)
}

func (r *endpointRepo) ListEndpointCounts(ctx context.Context, workspaceID string) ([]models.ApplicationEndpointCount, error) {
	query := `
	SELECT a.id AS application_id, a.name, count(e.id)::int AS endpoints
	FROM applications a LEFT JOIN endpoints e ON e.application_id = a.id AND e.archived_at IS NULL
	WHERE a.workspace_id = @workspace_id AND a.archived_at IS NULL
	GROUP BY a.id, a.name ORDER BY a.id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	counts, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.ApplicationEndpointCount])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return counts, nil
}
