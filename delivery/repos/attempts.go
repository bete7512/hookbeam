package repos

//go:generate mockgen -source=attempts.go -destination=repo_mocks/mock_attempts.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// AttemptRepo records network attempts.
type AttemptRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) AttemptRepo
	CreateAttempt(ctx context.Context, a *models.Attempt) error
	// ListAttempts returns a delivery's attempts, oldest first.
	ListAttempts(ctx context.Context, deliveryID int64) ([]models.Attempt, error)
}

type attemptRepo struct {
	db db.DBTX
}

// NewAttemptRepo returns an AttemptRepo over a pool or a transaction.
func NewAttemptRepo(conn db.DBTX) (AttemptRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &attemptRepo{db: conn}, nil
}

func (r *attemptRepo) WithTx(tx db.DBTX) AttemptRepo { return &attemptRepo{db: tx} }

const attemptColumns = `id, delivery_id, number, endpoint_revision, secret_version, request_url, started_at, completed_at, latency_ms, status_code, error_class, response_headers, response_preview`

func (r *attemptRepo) CreateAttempt(ctx context.Context, a *models.Attempt) error {
	query := `
	INSERT INTO attempts (delivery_id, number, endpoint_revision, secret_version, request_url, started_at, completed_at, latency_ms, status_code, error_class, response_headers, response_preview)
	VALUES (@delivery_id, @number, @endpoint_revision, @secret_version, @request_url, @started_at, @completed_at, @latency_ms, @status_code, @error_class, COALESCE(@response_headers, '{}'::jsonb), @response_preview)
	RETURNING ` + attemptColumns
	args := pgx.NamedArgs{
		"delivery_id":       a.DeliveryID,
		"number":            a.Number,
		"endpoint_revision": a.EndpointRevision,
		"secret_version":    a.SecretVersion,
		"request_url":       a.RequestURL,
		"started_at":        a.StartedAt,
		"completed_at":      a.CompletedAt,
		"latency_ms":        a.LatencyMS,
		"status_code":       a.StatusCode,
		"error_class":       a.ErrorClass,
		"response_headers":  a.ResponseHeaders,
		"response_preview":  a.ResponsePreview,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Attempt])
	if err != nil {
		return db.HandleError(err)
	}
	*a = created
	return nil
}

func (r *attemptRepo) ListAttempts(ctx context.Context, deliveryID int64) ([]models.Attempt, error) {
	rows, err := r.db.Query(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE delivery_id = @delivery_id ORDER BY number`, pgx.NamedArgs{"delivery_id": deliveryID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Attempt])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}
