package repos

//go:generate mockgen -source=audit.go -destination=repo_mocks/mock_audit.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// AuditRepo stores audit events.
type AuditRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) AuditRepo
	CreateAuditEvent(ctx context.Context, e *models.AuditEvent) error
	// ListAuditEvents returns a page of the workspace's events, newest first.
	ListAuditEvents(ctx context.Context, opts ListAuditOpts) ([]models.AuditEvent, error)
	CountAuditEvents(ctx context.Context, opts ListAuditOpts) (int, error)
	// DeleteAuditEventsBefore deletes up to limit events older than the cutoff and returns how many.
	DeleteAuditEventsBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

// ListAuditOpts filters and pages the audit list; zero values mean "any".
type ListAuditOpts struct {
	WorkspaceID  string
	ActorID      string
	ResourceType string
	ResourceID   int64
	After        *time.Time
	Before       *time.Time
	Limit        int
	Offset       int
}

type auditRepo struct {
	db db.DBTX
}

// NewAuditRepo returns an AuditRepo over a pool or a transaction.
func NewAuditRepo(conn db.DBTX) (AuditRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &auditRepo{db: conn}, nil
}

func (r *auditRepo) WithTx(tx db.DBTX) AuditRepo { return &auditRepo{db: tx} }

const (
	auditColumns = `id, workspace_id, actor_id, action, resource_type, resource_id, request_id, details, occurred_at`
	auditFilter  = `workspace_id = @workspace_id
	AND (@actor_id::text = '' OR actor_id = @actor_id)
	AND (@resource_type::text = '' OR resource_type = @resource_type)
	AND (@resource_id::bigint = 0 OR resource_id = @resource_id)
	AND (@after::timestamptz IS NULL OR occurred_at >= @after)
	AND (@before::timestamptz IS NULL OR occurred_at < @before)`
)

func auditArgs(opts ListAuditOpts) pgx.NamedArgs {
	return pgx.NamedArgs{"workspace_id": opts.WorkspaceID, "actor_id": opts.ActorID, "resource_type": opts.ResourceType, "resource_id": opts.ResourceID, "after": opts.After, "before": opts.Before, "limit": opts.Limit, "offset": opts.Offset}
}

func (r *auditRepo) CreateAuditEvent(ctx context.Context, e *models.AuditEvent) error {
	details := e.Details
	if len(details) == 0 {
		details = []byte("{}")
	}
	query := `INSERT INTO audit_events (workspace_id, actor_id, action, resource_type, resource_id, request_id, details) VALUES (@workspace_id, @actor_id, @action, @resource_type, @resource_id, @request_id, @details) RETURNING ` + auditColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": e.WorkspaceID, "actor_id": e.ActorID, "action": e.Action, "resource_type": e.ResourceType, "resource_id": e.ResourceID, "request_id": e.RequestID, "details": details})
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByNameLax[models.AuditEvent])
	if err != nil {
		return db.HandleError(err)
	}
	*e = created
	return nil
}

func (r *auditRepo) ListAuditEvents(ctx context.Context, opts ListAuditOpts) ([]models.AuditEvent, error) {
	rows, err := r.db.Query(ctx, `SELECT `+auditColumns+` FROM audit_events WHERE `+auditFilter+` ORDER BY occurred_at DESC, id DESC LIMIT @limit OFFSET @offset`, auditArgs(opts))
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[models.AuditEvent])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}

func (r *auditRepo) CountAuditEvents(ctx context.Context, opts ListAuditOpts) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE `+auditFilter, auditArgs(opts)).Scan(&n)
	return n, db.HandleError(err)
}

func (r *auditRepo) DeleteAuditEventsBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM audit_events WHERE id IN (SELECT id FROM audit_events WHERE occurred_at < @before ORDER BY occurred_at LIMIT @limit)`, pgx.NamedArgs{"before": before, "limit": limit})
	if err != nil {
		return 0, db.HandleError(err)
	}
	return tag.RowsAffected(), nil
}
