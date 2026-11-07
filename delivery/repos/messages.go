package repos

//go:generate mockgen -source=messages.go -destination=repo_mocks/mock_messages.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// MessageRepo reads and writes accepted messages.
type MessageRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) MessageRepo
	CreateMessage(ctx context.Context, m *models.Message) error
	// GetMessage returns a message of the workspace.
	GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, error)
	// GetMessageByID returns a message without a workspace check; for the worker.
	GetMessageByID(ctx context.Context, id int64) (*models.Message, error)
	// FindMessageByIdempotencyKey returns the application's message accepted under the key, or ErrNotFound.
	FindMessageByIdempotencyKey(ctx context.Context, applicationID int64, key string) (*models.Message, error)
	// ExpirePayloads empties up to limit payloads past payload_expires_at and returns how many; metadata stays.
	ExpirePayloads(ctx context.Context, limit int) (int64, error)
	// ExpireIdempotencyKeys clears up to limit idempotency keys past idempotency_expires_at and returns how many,
	// so the same key accepted again is a new submission.
	ExpireIdempotencyKeys(ctx context.Context, limit int) (int64, error)
}

type messageRepo struct {
	db db.DBTX
}

// NewMessageRepo returns a MessageRepo over a pool or a transaction.
func NewMessageRepo(conn db.DBTX) (MessageRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &messageRepo{db: conn}, nil
}

func (r *messageRepo) WithTx(tx db.DBTX) MessageRepo { return &messageRepo{db: tx} }

const messageColumns = `id, public_id, workspace_id, application_id, event_type, sender_event_id, idempotency_key, idempotency_expires_at, payload, payload_sha256, payload_expires_at, accepted_at`

func (r *messageRepo) CreateMessage(ctx context.Context, m *models.Message) error {
	query := `
	INSERT INTO messages (public_id, workspace_id, application_id, event_type, sender_event_id, idempotency_key, idempotency_expires_at, payload, payload_sha256, payload_expires_at)
	VALUES (@public_id, @workspace_id, @application_id, @event_type, @sender_event_id, @idempotency_key, @idempotency_expires_at, @payload, @payload_sha256, @payload_expires_at)
	RETURNING ` + messageColumns
	args := pgx.NamedArgs{
		"public_id":              m.PublicID,
		"workspace_id":           m.WorkspaceID,
		"application_id":         m.ApplicationID,
		"event_type":             m.EventType,
		"sender_event_id":        m.SenderEventID,
		"idempotency_key":        m.IdempotencyKey,
		"idempotency_expires_at": m.IdempotencyExpiresAt,
		"payload":                m.Payload,
		"payload_sha256":         m.PayloadSHA256,
		"payload_expires_at":     m.PayloadExpiresAt,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Message])
	if err != nil {
		return db.HandleError(err)
	}
	*m = created
	return nil
}

func (r *messageRepo) GetMessage(ctx context.Context, workspaceID string, id int64) (*models.Message, error) {
	return r.one(ctx, `SELECT `+messageColumns+` FROM messages WHERE workspace_id = @workspace_id AND id = @id`, pgx.NamedArgs{"workspace_id": workspaceID, "id": id})
}

func (r *messageRepo) GetMessageByID(ctx context.Context, id int64) (*models.Message, error) {
	return r.one(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = @id`, pgx.NamedArgs{"id": id})
}

func (r *messageRepo) FindMessageByIdempotencyKey(ctx context.Context, applicationID int64, key string) (*models.Message, error) {
	query := `SELECT ` + messageColumns + ` FROM messages WHERE application_id = @application_id AND idempotency_key = @key`
	return r.one(ctx, query, pgx.NamedArgs{"application_id": applicationID, "key": key})
}

func (r *messageRepo) one(ctx context.Context, query string, args pgx.NamedArgs) (*models.Message, error) {
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	m, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[models.Message])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return m, nil
}

func (r *messageRepo) ExpirePayloads(ctx context.Context, limit int) (int64, error) {
	query := `
	UPDATE messages SET payload = ''::bytea
	WHERE id IN (SELECT id FROM messages WHERE payload_expires_at <= now() AND octet_length(payload) > 0 ORDER BY payload_expires_at LIMIT @limit)`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"limit": limit})
	if err != nil {
		return 0, db.HandleError(err)
	}
	return tag.RowsAffected(), nil
}

func (r *messageRepo) ExpireIdempotencyKeys(ctx context.Context, limit int) (int64, error) {
	query := `
	UPDATE messages SET idempotency_key = NULL, idempotency_expires_at = NULL
	WHERE id IN (SELECT id FROM messages WHERE idempotency_key IS NOT NULL AND idempotency_expires_at <= now() ORDER BY idempotency_expires_at LIMIT @limit)`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"limit": limit})
	if err != nil {
		return 0, db.HandleError(err)
	}
	return tag.RowsAffected(), nil
}
