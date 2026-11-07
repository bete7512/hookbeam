package repos

//go:generate mockgen -source=deliveries.go -destination=repo_mocks/mock_deliveries.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

// DeliveryRepo owns the schedule: planned deliveries, claims and outcomes.
type DeliveryRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) DeliveryRepo
	CreateDelivery(ctx context.Context, d *models.Delivery) error
	// ListDeliveriesByMessage returns the message's deliveries in id order.
	ListDeliveriesByMessage(ctx context.Context, messageID int64) ([]models.Delivery, error)
	// GetDelivery returns a delivery whose message belongs to the workspace.
	GetDelivery(ctx context.Context, workspaceID string, id int64) (*models.Delivery, error)
	// ClaimDueDeliveries moves up to limit due deliveries of unpaused, unarchived endpoints to in_flight under the
	// lease, increments their attempt count and returns them; rows another worker holds are skipped.
	ClaimDueDeliveries(ctx context.Context, limit int, leaseToken string, leaseTTL time.Duration) ([]models.Delivery, error)
	// RecordOutcome applies the outcome of an attempt if the lease is still held; ErrNotFound means it was lost.
	RecordOutcome(ctx context.Context, o models.DeliveryOutcome) error
	// CountDeliveriesByMessage returns how many deliveries a message has.
	CountDeliveriesByMessage(ctx context.Context, messageID int64) (int32, error)
}

type deliveryRepo struct {
	db db.DBTX
}

// NewDeliveryRepo returns a DeliveryRepo over a pool or a transaction.
func NewDeliveryRepo(conn db.DBTX) (DeliveryRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &deliveryRepo{db: conn}, nil
}

func (r *deliveryRepo) WithTx(tx db.DBTX) DeliveryRepo { return &deliveryRepo{db: tx} }

const deliveryColumns = `id, message_id, endpoint_id, endpoint_revision, state, attempt_count, due_at, deadline_at, lease_token, lease_expires_at, cancel_requested, failure_reason, replay_of_delivery_id, replay_idempotency_key, completed_at, created_at, updated_at`

func (r *deliveryRepo) CreateDelivery(ctx context.Context, d *models.Delivery) error {
	query := `
	INSERT INTO deliveries (message_id, endpoint_id, endpoint_revision, state, due_at, deadline_at, replay_of_delivery_id, replay_idempotency_key)
	VALUES (@message_id, @endpoint_id, @endpoint_revision, @state, @due_at, @deadline_at, @replay_of_delivery_id, @replay_idempotency_key)
	RETURNING ` + deliveryColumns
	args := pgx.NamedArgs{
		"message_id":             d.MessageID,
		"endpoint_id":            d.EndpointID,
		"endpoint_revision":      d.EndpointRevision,
		"state":                  d.State,
		"due_at":                 d.DueAt,
		"deadline_at":            d.DeadlineAt,
		"replay_of_delivery_id":  d.ReplayOfDeliveryID,
		"replay_idempotency_key": d.ReplayIdempotencyKey,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Delivery])
	if err != nil {
		return db.HandleError(err)
	}
	*d = created
	return nil
}

func (r *deliveryRepo) ListDeliveriesByMessage(ctx context.Context, messageID int64) ([]models.Delivery, error) {
	rows, err := r.db.Query(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE message_id = @message_id ORDER BY id`, pgx.NamedArgs{"message_id": messageID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Delivery])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *deliveryRepo) CountDeliveriesByMessage(ctx context.Context, messageID int64) (int32, error) {
	var n int32
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE message_id = @message_id`, pgx.NamedArgs{"message_id": messageID}).Scan(&n)
	return n, db.HandleError(err)
}

func (r *deliveryRepo) GetDelivery(ctx context.Context, workspaceID string, id int64) (*models.Delivery, error) {
	query := `
	SELECT ` + prefixed(deliveryColumns, "d.") + `
	FROM deliveries d JOIN messages m ON m.id = d.message_id
	WHERE d.id = @id AND m.workspace_id = @workspace_id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"id": id, "workspace_id": workspaceID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	d, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Delivery])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return d, nil
}

func (r *deliveryRepo) ClaimDueDeliveries(ctx context.Context, limit int, leaseToken string, leaseTTL time.Duration) ([]models.Delivery, error) {
	query := `
	UPDATE deliveries SET state = 'in_flight', lease_token = @token, lease_expires_at = now() + @ttl, attempt_count = attempt_count + 1
	WHERE id IN (
		SELECT d.id FROM deliveries d
		JOIN endpoints e ON e.id = d.endpoint_id
		WHERE d.state IN ('queued', 'retry_scheduled') AND d.due_at <= now() AND NOT d.cancel_requested
		  AND e.paused_at IS NULL AND e.archived_at IS NULL
		ORDER BY d.due_at, d.id
		LIMIT @limit
		FOR UPDATE OF d SKIP LOCKED
	)
	RETURNING ` + deliveryColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"token": leaseToken, "ttl": leaseTTL, "limit": limit})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.Delivery])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *deliveryRepo) RecordOutcome(ctx context.Context, o models.DeliveryOutcome) error {
	query := `
	UPDATE deliveries
	SET state = @state, due_at = @due_at, failure_reason = @failure_reason, lease_token = NULL, lease_expires_at = NULL,
	    completed_at = CASE WHEN @state IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE NULL END
	WHERE id = @id AND lease_token = @token AND state = 'in_flight'`
	args := pgx.NamedArgs{"id": o.DeliveryID, "token": o.LeaseToken, "state": o.State, "due_at": o.DueAt, "failure_reason": o.FailureReason}
	tag, err := r.db.Exec(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// prefixed qualifies a column list with a table alias.
func prefixed(columns, prefix string) string {
	out := prefix
	for _, c := range columns {
		out += string(c)
		if c == ',' {
			out += " " + prefix
		}
	}
	return out
}
