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
	// ReleaseDelivery gives a claimed delivery back to the schedule before any attempt was made: the lease is
	// cleared, the attempt count undone and the delivery becomes due when the lease would have expired.
	// ErrNotFound means the lease was not held.
	ReleaseDelivery(ctx context.Context, id int64, leaseToken string) error
	// ClaimExpiredLeases re-leases up to limit in_flight deliveries whose lease has expired, in expiry order,
	// and returns them with the time the lost lease ended; rows another sweeper holds are skipped.
	ClaimExpiredLeases(ctx context.Context, limit int, leaseToken string, leaseTTL time.Duration) ([]models.ExpiredDelivery, error)
	// CancelDelivery cancels a queued or retry-scheduled delivery of the workspace now, or marks an in-flight one
	// cancel_requested so its attempt settles first. ErrNotFound means no such active delivery in the workspace.
	CancelDelivery(ctx context.Context, workspaceID string, id int64) (*models.Delivery, error)
	// FindReplay returns the replay of a delivery created under the idempotency key, or ErrNotFound.
	FindReplay(ctx context.Context, replayOfDeliveryID int64, idempotencyKey string) (*models.Delivery, error)
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

// RecordOutcome honours a cancel requested during the attempt: a success still counts, anything else ends the
// cycle as cancelled with the attempt's real outcome on the attempt row.
func (r *deliveryRepo) RecordOutcome(ctx context.Context, o models.DeliveryOutcome) error {
	query := `
	UPDATE deliveries
	SET state = CASE WHEN cancel_requested AND @state::text <> 'succeeded' THEN 'cancelled' ELSE @state::text END,
	    due_at = CASE WHEN cancel_requested AND @state::text <> 'succeeded' THEN NULL ELSE @due_at::timestamptz END,
	    failure_reason = CASE WHEN cancel_requested AND @state::text <> 'succeeded' THEN 'cancelled' ELSE @failure_reason::text END,
	    lease_token = NULL, lease_expires_at = NULL,
	    completed_at = CASE WHEN cancel_requested AND @state::text <> 'succeeded' OR @state::text IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE NULL END
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

func (r *deliveryRepo) ReleaseDelivery(ctx context.Context, id int64, leaseToken string) error {
	query := `
	UPDATE deliveries
	SET state = CASE WHEN cancel_requested THEN 'cancelled' WHEN attempt_count <= 1 THEN 'queued' ELSE 'retry_scheduled' END,
	    attempt_count = attempt_count - 1,
	    due_at = CASE WHEN cancel_requested THEN NULL ELSE lease_expires_at END,
	    failure_reason = CASE WHEN cancel_requested THEN 'cancelled' ELSE failure_reason END,
	    completed_at = CASE WHEN cancel_requested THEN now() ELSE NULL END,
	    lease_token = NULL, lease_expires_at = NULL
	WHERE id = @id AND lease_token = @token AND state = 'in_flight'`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id, "token": leaseToken})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *deliveryRepo) ClaimExpiredLeases(ctx context.Context, limit int, leaseToken string, leaseTTL time.Duration) ([]models.ExpiredDelivery, error) {
	query := `
	WITH expired AS (
		SELECT id, lease_expires_at AS lease_expired_at FROM deliveries
		WHERE state = 'in_flight' AND lease_expires_at <= now()
		ORDER BY lease_expires_at, id
		LIMIT @limit
		FOR UPDATE SKIP LOCKED
	)
	UPDATE deliveries d SET lease_token = @token, lease_expires_at = now() + @ttl
	FROM expired
	WHERE d.id = expired.id
	RETURNING ` + prefixed(deliveryColumns, "d.") + `, expired.lease_expired_at`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"token": leaseToken, "ttl": leaseTTL, "limit": limit})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.ExpiredDelivery])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *deliveryRepo) CancelDelivery(ctx context.Context, workspaceID string, id int64) (*models.Delivery, error) {
	query := `
	UPDATE deliveries d
	SET state = CASE WHEN d.state = 'in_flight' THEN d.state ELSE 'cancelled' END,
	    cancel_requested = CASE WHEN d.state = 'in_flight' THEN true ELSE d.cancel_requested END,
	    due_at = CASE WHEN d.state = 'in_flight' THEN d.due_at ELSE NULL END,
	    failure_reason = CASE WHEN d.state = 'in_flight' THEN d.failure_reason ELSE 'cancelled' END,
	    completed_at = CASE WHEN d.state = 'in_flight' THEN NULL ELSE now() END
	FROM messages m
	WHERE d.id = @id AND m.id = d.message_id AND m.workspace_id = @workspace_id
	  AND d.state IN ('queued', 'retry_scheduled', 'in_flight')
	RETURNING ` + prefixed(deliveryColumns, "d.")
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

func (r *deliveryRepo) FindReplay(ctx context.Context, replayOfDeliveryID int64, idempotencyKey string) (*models.Delivery, error) {
	query := `SELECT ` + deliveryColumns + ` FROM deliveries WHERE replay_of_delivery_id = @original AND replay_idempotency_key = @key`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"original": replayOfDeliveryID, "key": idempotencyKey})
	if err != nil {
		return nil, db.HandleError(err)
	}
	d, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Delivery])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return d, nil
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
