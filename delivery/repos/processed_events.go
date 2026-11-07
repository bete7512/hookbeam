package repos

//go:generate mockgen -source=processed_events.go -destination=repo_mocks/mock_processed_events.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/pkg/db"
)

// ProcessedEventRepo records which events the worker has handled.
type ProcessedEventRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) ProcessedEventRepo
	// MarkEventProcessed records eventID and reports whether it was new; false means it was already processed.
	MarkEventProcessed(ctx context.Context, eventID int64) (bool, error)
}

type processedEventRepo struct {
	db db.DBTX
}

// NewProcessedEventRepo returns a ProcessedEventRepo over a pool or a transaction.
func NewProcessedEventRepo(conn db.DBTX) (ProcessedEventRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &processedEventRepo{db: conn}, nil
}

func (r *processedEventRepo) WithTx(tx db.DBTX) ProcessedEventRepo {
	return &processedEventRepo{db: tx}
}

func (r *processedEventRepo) MarkEventProcessed(ctx context.Context, eventID int64) (bool, error) {
	query := `INSERT INTO processed_events (event_id) VALUES (@event_id) ON CONFLICT DO NOTHING`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"event_id": eventID})
	if err != nil {
		return false, db.HandleError(err)
	}
	return tag.RowsAffected() == 1, nil
}
