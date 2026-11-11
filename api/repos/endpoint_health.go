package repos

//go:generate mockgen -source=endpoint_health.go -destination=repo_mocks/mock_endpoint_health.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// EndpointHealthRepo keeps one health row per endpoint.
type EndpointHealthRepo interface {
	// ObserveEndpointHealth applies an observation: a success resets the failure streak, a counted failure
	// extends it, anything else only refreshes the last outcome. An observation older than the row's is ignored.
	ObserveEndpointHealth(ctx context.Context, o models.HealthObservation) error
	// GetEndpointHealth returns the row of an endpoint in the workspace.
	GetEndpointHealth(ctx context.Context, workspaceID string, endpointID int64) (*models.EndpointHealth, error)
}

type endpointHealthRepo struct {
	db db.DBTX
}

// NewEndpointHealthRepo returns an EndpointHealthRepo over a pool or a transaction.
func NewEndpointHealthRepo(conn db.DBTX) (EndpointHealthRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &endpointHealthRepo{db: conn}, nil
}

const endpointHealthColumns = `endpoint_id, workspace_id, application_id, consecutive_failures, last_state, last_reason, last_status_code, last_delivery_id, last_attempt, observed_at, created_at, updated_at`

func (r *endpointHealthRepo) ObserveEndpointHealth(ctx context.Context, o models.HealthObservation) error {
	query := `
	INSERT INTO endpoint_health (endpoint_id, workspace_id, application_id, consecutive_failures, last_state, last_reason, last_status_code, last_delivery_id, last_attempt, observed_at)
	VALUES (@endpoint_id, @workspace_id, @application_id, CASE WHEN @failed::bool THEN 1 ELSE 0 END, @state, @reason, @status_code, @delivery_id, @attempt, @observed_at)
	ON CONFLICT (endpoint_id) DO UPDATE SET
	  consecutive_failures = CASE WHEN EXCLUDED.last_state = 'succeeded' THEN 0 WHEN @failed::bool THEN endpoint_health.consecutive_failures + 1 ELSE endpoint_health.consecutive_failures END,
	  last_state = EXCLUDED.last_state, last_reason = EXCLUDED.last_reason, last_status_code = EXCLUDED.last_status_code,
	  last_delivery_id = EXCLUDED.last_delivery_id, last_attempt = EXCLUDED.last_attempt, observed_at = EXCLUDED.observed_at
	WHERE EXCLUDED.observed_at > endpoint_health.observed_at`
	args := pgx.NamedArgs{
		"endpoint_id":    o.EndpointID,
		"workspace_id":   o.WorkspaceID,
		"application_id": o.ApplicationID,
		"failed":         o.CountsAsFailed,
		"state":          o.State,
		"reason":         o.Reason,
		"status_code":    o.StatusCode,
		"delivery_id":    o.DeliveryID,
		"attempt":        o.Attempt,
		"observed_at":    o.ObservedAt,
	}
	_, err := r.db.Exec(ctx, query, args)
	return db.HandleError(err)
}

func (r *endpointHealthRepo) GetEndpointHealth(ctx context.Context, workspaceID string, endpointID int64) (*models.EndpointHealth, error) {
	rows, err := r.db.Query(ctx, `SELECT `+endpointHealthColumns+` FROM endpoint_health WHERE workspace_id = @workspace_id AND endpoint_id = @endpoint_id`, pgx.NamedArgs{
		"workspace_id": workspaceID,
		"endpoint_id":  endpointID,
	})
	if err != nil {
		return nil, db.HandleError(err)
	}
	h, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.EndpointHealth])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return h, nil
}
