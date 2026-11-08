package repos

//go:generate mockgen -source=health.go -destination=repo_mocks/mock_health.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

// HealthRepo keeps the per-endpoint failure streak and its episodes.
type HealthRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) HealthRepo
	// ObserveEndpointHealth applies one observation and returns the row after it, locked for the transaction:
	// a success resets the streak, a failure extends it. An observation whose revision is older than the
	// row's is ignored and reported as ErrNotFound.
	ObserveEndpointHealth(ctx context.Context, o models.HealthObservation) (*models.EndpointHealth, error)
	// GetEndpointHealth returns the row of an endpoint in the workspace, with its open episode if any.
	GetEndpointHealth(ctx context.Context, workspaceID string, endpointID int64) (*models.EndpointHealth, error)
	// GetEpisode returns one episode of the workspace.
	GetEpisode(ctx context.Context, workspaceID string, id int64) (*models.HealthEpisode, error)
	// OpenEpisode starts an episode; a second open episode for the endpoint is ErrConflict.
	OpenEpisode(ctx context.Context, e *models.HealthEpisode) error
	// ContinueEpisode records a longer streak on an open episode.
	ContinueEpisode(ctx context.Context, id int64, consecutiveFailures int32) error
	// ResolveEpisode closes an open episode at the given time.
	ResolveEpisode(ctx context.Context, id int64, at time.Time) error
}

type healthRepo struct {
	db db.DBTX
}

// NewHealthRepo returns a HealthRepo over a pool or a transaction.
func NewHealthRepo(conn db.DBTX) (HealthRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &healthRepo{db: conn}, nil
}

func (r *healthRepo) WithTx(tx db.DBTX) HealthRepo { return &healthRepo{db: tx} }

const endpointHealthColumns = `h.endpoint_id, h.workspace_id, h.application_id, h.revision, h.consecutive_failures, h.last_state, h.observed_at,
	(SELECT e.id FROM health_episodes e WHERE e.endpoint_id = h.endpoint_id AND e.resolved_at IS NULL) AS open_episode_id`

// ObserveEndpointHealth runs two statements on purpose: the upsert takes the row lock (and may wait for a
// concurrent observer to commit), and only a statement started after that wait sees the episode the other
// observer opened. One statement would read health_episodes from a snapshot taken before the lock.
func (r *healthRepo) ObserveEndpointHealth(ctx context.Context, o models.HealthObservation) (*models.EndpointHealth, error) {
	query := `
	INSERT INTO endpoint_health AS h (endpoint_id, workspace_id, application_id, revision, consecutive_failures, last_state, observed_at)
	VALUES (@endpoint_id, @workspace_id, @application_id, @revision, CASE WHEN @failed::bool THEN 1 ELSE 0 END, CASE WHEN @failed::bool THEN 'failed' ELSE 'succeeded' END, @observed_at)
	ON CONFLICT (endpoint_id) DO UPDATE SET
	  revision = EXCLUDED.revision,
	  consecutive_failures = CASE WHEN @failed::bool THEN h.consecutive_failures + 1 ELSE 0 END,
	  last_state = EXCLUDED.last_state,
	  observed_at = EXCLUDED.observed_at
	WHERE EXCLUDED.revision >= h.revision
	RETURNING h.endpoint_id, h.workspace_id, h.application_id, h.revision, h.consecutive_failures, h.last_state, h.observed_at`
	args := pgx.NamedArgs{"endpoint_id": o.EndpointID, "workspace_id": o.WorkspaceID, "application_id": o.ApplicationID, "revision": o.Revision, "failed": o.Failed, "observed_at": o.ObservedAt}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	h, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[models.EndpointHealth])
	if err != nil {
		return nil, db.HandleError(err)
	}
	var open *int64
	err = r.db.QueryRow(ctx, `SELECT id FROM health_episodes WHERE endpoint_id = @endpoint_id AND resolved_at IS NULL`, pgx.NamedArgs{"endpoint_id": o.EndpointID}).Scan(&open)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, db.HandleError(err)
	}
	h.OpenEpisodeID = open
	return h, nil
}

func (r *healthRepo) GetEndpointHealth(ctx context.Context, workspaceID string, endpointID int64) (*models.EndpointHealth, error) {
	query := `SELECT ` + endpointHealthColumns + ` FROM endpoint_health h WHERE h.workspace_id = @workspace_id AND h.endpoint_id = @endpoint_id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "endpoint_id": endpointID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	h, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.EndpointHealth])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return h, nil
}

const healthEpisodeColumns = `id, endpoint_id, workspace_id, revision, consecutive_failures, opened_at, resolved_at, created_at`

func (r *healthRepo) GetEpisode(ctx context.Context, workspaceID string, id int64) (*models.HealthEpisode, error) {
	rows, err := r.db.Query(ctx, `SELECT `+healthEpisodeColumns+` FROM health_episodes WHERE workspace_id = @workspace_id AND id = @id`, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
	if err != nil {
		return nil, db.HandleError(err)
	}
	e, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.HealthEpisode])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return e, nil
}

func (r *healthRepo) OpenEpisode(ctx context.Context, e *models.HealthEpisode) error {
	query := `
	INSERT INTO health_episodes (endpoint_id, workspace_id, revision, consecutive_failures, opened_at)
	VALUES (@endpoint_id, @workspace_id, @revision, @consecutive_failures, @opened_at)
	RETURNING ` + healthEpisodeColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"endpoint_id": e.EndpointID, "workspace_id": e.WorkspaceID, "revision": e.Revision, "consecutive_failures": e.ConsecutiveFailures, "opened_at": e.OpenedAt})
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.HealthEpisode])
	if err != nil {
		return db.HandleError(err)
	}
	*e = created
	return nil
}

func (r *healthRepo) ContinueEpisode(ctx context.Context, id int64, consecutiveFailures int32) error {
	tag, err := r.db.Exec(ctx, `UPDATE health_episodes SET consecutive_failures = @n WHERE id = @id AND resolved_at IS NULL`, pgx.NamedArgs{"id": id, "n": consecutiveFailures})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *healthRepo) ResolveEpisode(ctx context.Context, id int64, at time.Time) error {
	tag, err := r.db.Exec(ctx, `UPDATE health_episodes SET resolved_at = @at WHERE id = @id AND resolved_at IS NULL`, pgx.NamedArgs{"id": id, "at": at})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}
