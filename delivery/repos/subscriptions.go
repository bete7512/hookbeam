package repos

//go:generate mockgen -source=subscriptions.go -destination=repo_mocks/mock_subscriptions.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/pkg/db"
)

// SubscriptionRepo stores which event types an endpoint receives.
type SubscriptionRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) SubscriptionRepo
	// CreateSubscriptions adds one row per event type for the endpoint.
	CreateSubscriptions(ctx context.Context, endpointID int64, eventTypes []string) error
	// ReplaceSubscriptions makes eventTypes the endpoint's whole subscription list.
	ReplaceSubscriptions(ctx context.Context, endpointID int64, eventTypes []string) error
	// ListEventTypes returns the endpoint's event types sorted.
	ListEventTypes(ctx context.Context, endpointID int64) ([]string, error)
}

type subscriptionRepo struct {
	db db.DBTX
}

// NewSubscriptionRepo returns a SubscriptionRepo over a pool or a transaction.
func NewSubscriptionRepo(conn db.DBTX) (SubscriptionRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &subscriptionRepo{db: conn}, nil
}

func (r *subscriptionRepo) WithTx(tx db.DBTX) SubscriptionRepo { return &subscriptionRepo{db: tx} }

func (r *subscriptionRepo) CreateSubscriptions(ctx context.Context, endpointID int64, eventTypes []string) error {
	if len(eventTypes) == 0 {
		return nil
	}
	query := `INSERT INTO subscriptions (endpoint_id, event_type) SELECT @endpoint_id, unnest(@event_types::text[])`
	_, err := r.db.Exec(ctx, query, pgx.NamedArgs{"endpoint_id": endpointID, "event_types": eventTypes})
	return db.HandleError(err)
}

func (r *subscriptionRepo) ReplaceSubscriptions(ctx context.Context, endpointID int64, eventTypes []string) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM subscriptions WHERE endpoint_id = @endpoint_id`, pgx.NamedArgs{"endpoint_id": endpointID}); err != nil {
		return db.HandleError(err)
	}
	return r.CreateSubscriptions(ctx, endpointID, eventTypes)
}

func (r *subscriptionRepo) ListEventTypes(ctx context.Context, endpointID int64) ([]string, error) {
	query := `SELECT event_type FROM subscriptions WHERE endpoint_id = @endpoint_id ORDER BY event_type`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"endpoint_id": endpointID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	types, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return types, nil
}
