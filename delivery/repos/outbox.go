package repos

//go:generate mockgen -source=outbox.go -destination=repo_mocks/mock_outbox.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// OutboxRepo reads and writes the outbox.
type OutboxRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) OutboxRepo
	CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error
	// ClaimOutboxEvents locks up to limit unpublished events in id order, skipping rows another transaction holds; call it through WithTx.
	ClaimOutboxEvents(ctx context.Context, limit int) ([]models.OutboxEvent, error)
	MarkOutboxEventsPublished(ctx context.Context, ids []int64) error
	// OldestUnpublishedAt is when the oldest unpublished event occurred, or nil with an empty outbox.
	OldestUnpublishedAt(ctx context.Context) (*time.Time, error)
	// DeleteOutboxEventsPublishedBefore deletes up to limit events published before the cutoff and returns how many it deleted.
	DeleteOutboxEventsPublishedBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

type outboxRepo struct {
	db db.DBTX
}

// NewOutboxRepo returns an OutboxRepo over a pool or a transaction.
func NewOutboxRepo(conn db.DBTX) (OutboxRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &outboxRepo{db: conn}, nil
}

func (r *outboxRepo) WithTx(tx db.DBTX) OutboxRepo { return &outboxRepo{db: tx} }

const outboxColumns = `id, type, payload, occurred_at, published_at, trace_parent`

func (r *outboxRepo) CreateOutboxEvent(ctx context.Context, e *models.OutboxEvent) error {
	if e.TraceParent == "" {
		e.TraceParent = telemetry.TraceParent(ctx)
	}
	query := `
	INSERT INTO outbox (type, payload, occurred_at, trace_parent)
	VALUES (@type, @payload, COALESCE(@occurred_at, now()), @trace_parent)
	RETURNING id, occurred_at
	`
	var occurredAt *time.Time
	if !e.OccurredAt.IsZero() {
		occurredAt = &e.OccurredAt
	}
	args := pgx.NamedArgs{
		"type":         e.Type,
		"payload":      e.Payload,
		"occurred_at":  occurredAt,
		"trace_parent": e.TraceParent,
	}
	err := r.db.QueryRow(ctx, query, args).Scan(&e.ID, &e.OccurredAt)
	return db.HandleError(err)
}

func (r *outboxRepo) ClaimOutboxEvents(ctx context.Context, limit int) ([]models.OutboxEvent, error) {
	query := `
	SELECT ` + outboxColumns + `
	FROM outbox
	WHERE published_at IS NULL
	ORDER BY id
	LIMIT @limit
	FOR UPDATE SKIP LOCKED
	`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"limit": limit})
	if err != nil {
		return nil, db.HandleError(err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.OutboxEvent])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return events, nil
}

func (r *outboxRepo) OldestUnpublishedAt(ctx context.Context) (*time.Time, error) {
	var at *time.Time
	if err := r.db.QueryRow(ctx, `SELECT min(occurred_at) FROM outbox WHERE published_at IS NULL`).Scan(&at); err != nil {
		return nil, db.HandleError(err)
	}
	return at, nil
}

func (r *outboxRepo) MarkOutboxEventsPublished(ctx context.Context, ids []int64) error {
	query := `UPDATE outbox SET published_at = now() WHERE id = ANY(@ids)`
	_, err := r.db.Exec(ctx, query, pgx.NamedArgs{"ids": ids})
	return db.HandleError(err)
}

func (r *outboxRepo) DeleteOutboxEventsPublishedBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	query := `
	DELETE FROM outbox
	WHERE id IN (
		SELECT id FROM outbox
		WHERE published_at < @before
		LIMIT @limit
	)
	`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, db.HandleError(err)
	}
	return tag.RowsAffected(), nil
}
