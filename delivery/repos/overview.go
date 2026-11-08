package repos

//go:generate mockgen -source=overview.go -destination=repo_mocks/mock_overview.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// OverviewRepo reads the numbers behind an environment's overview tiles.
type OverviewRepo interface {
	// GetOverview counts the environment's accepted messages, succeeded and exhausted deliveries since `since`,
	// its current backlog and oldest pending delivery, and the health of its unarchived endpoints.
	GetOverview(ctx context.Context, workspaceID string, environmentID int64, since time.Time) (*models.Overview, error)
}

type overviewRepo struct {
	db db.DBTX
}

// NewOverviewRepo returns an OverviewRepo over a pool or a transaction.
func NewOverviewRepo(conn db.DBTX) (OverviewRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &overviewRepo{db: conn}, nil
}

func (r *overviewRepo) GetOverview(ctx context.Context, workspaceID string, environmentID int64, since time.Time) (*models.Overview, error) {
	query := `
	WITH apps AS (SELECT id FROM applications WHERE workspace_id = @workspace_id AND environment_id = @environment_id),
	     eps AS (SELECT id, archived_at FROM endpoints WHERE application_id IN (SELECT id FROM apps))
	SELECT
	  (SELECT count(*) FROM messages m WHERE m.application_id IN (SELECT id FROM apps) AND m.accepted_at >= @since) AS accepted,
	  (SELECT count(*) FROM deliveries d WHERE d.endpoint_id IN (SELECT id FROM eps) AND d.state = 'succeeded' AND d.completed_at >= @since) AS succeeded,
	  (SELECT count(*) FROM deliveries d WHERE d.endpoint_id IN (SELECT id FROM eps) AND d.state = 'failed' AND d.failure_reason = 'attempts_exhausted' AND d.completed_at >= @since) AS exhausted,
	  (SELECT count(*) FROM deliveries d WHERE d.endpoint_id IN (SELECT id FROM eps) AND d.state IN ('queued', 'retry_scheduled')) AS backlog,
	  (SELECT min(d.due_at) FROM deliveries d WHERE d.endpoint_id IN (SELECT id FROM eps) AND d.state IN ('queued', 'retry_scheduled')) AS oldest_pending_due_at,
	  (SELECT count(*)::int FROM eps e JOIN endpoint_health h ON h.endpoint_id = e.id WHERE e.archived_at IS NULL AND NOT EXISTS (SELECT 1 FROM health_episodes x WHERE x.endpoint_id = e.id AND x.resolved_at IS NULL)) AS endpoints_healthy,
	  (SELECT count(*)::int FROM eps e WHERE e.archived_at IS NULL AND EXISTS (SELECT 1 FROM health_episodes x WHERE x.endpoint_id = e.id AND x.resolved_at IS NULL)) AS endpoints_unhealthy,
	  (SELECT count(*)::int FROM eps e WHERE e.archived_at IS NULL AND NOT EXISTS (SELECT 1 FROM endpoint_health h WHERE h.endpoint_id = e.id)) AS endpoints_unknown,
	  (SELECT max(h.observed_at) FROM endpoint_health h WHERE h.endpoint_id IN (SELECT id FROM eps)) AS health_observed_at`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": workspaceID, "environment_id": environmentID, "since": since})
	if err != nil {
		return nil, db.HandleError(err)
	}
	o, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByNameLax[models.Overview])
	if err != nil {
		return nil, db.HandleError(err)
	}
	o.Since = since
	return o, nil
}
