package repos

//go:generate mockgen -source=auth_tokens.go -destination=repo_mocks/mock_auth_tokens.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/db"
)

// AuthTokenRepo stores single-use tokens such as email verification and password reset.
type AuthTokenRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) AuthTokenRepo
	// CreateAuthToken atomically advances the user/purpose slot for a newer request and returns the current slot in t.
	CreateAuthToken(ctx context.Context, t *models.AuthToken) error
	// ConsumeAuthToken marks the unused, unexpired token with this hash and purpose as used and returns it; otherwise apperr.ErrNotFound.
	ConsumeAuthToken(ctx context.Context, tokenHash []byte, purpose string) (*models.AuthToken, error)
}

type authTokenRepo struct {
	db db.DBTX
}

// NewAuthTokenRepo returns an AuthTokenRepo over a pool or a transaction.
func NewAuthTokenRepo(conn db.DBTX) (AuthTokenRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &authTokenRepo{db: conn}, nil
}

func (r *authTokenRepo) WithTx(tx db.DBTX) AuthTokenRepo { return &authTokenRepo{db: tx} }

const authTokenColumns = `id, user_id, request_id, purpose, token_hash, expires_at, used_at, created_at` //nolint:gosec // column list, not a credential

func (r *authTokenRepo) CreateAuthToken(ctx context.Context, t *models.AuthToken) error {
	query := `
	INSERT INTO auth_tokens (user_id, request_id, purpose, token_hash, expires_at)
	VALUES (@user_id, @request_id, @purpose, @token_hash, @expires_at)
	ON CONFLICT (user_id, purpose) WHERE request_id IS NOT NULL DO UPDATE
	SET token_hash = CASE WHEN auth_tokens.request_id < EXCLUDED.request_id THEN EXCLUDED.token_hash ELSE auth_tokens.token_hash END,
	    expires_at = CASE WHEN auth_tokens.request_id < EXCLUDED.request_id THEN EXCLUDED.expires_at ELSE auth_tokens.expires_at END,
	    used_at = CASE WHEN auth_tokens.request_id < EXCLUDED.request_id THEN NULL ELSE auth_tokens.used_at END,
	    created_at = CASE WHEN auth_tokens.request_id < EXCLUDED.request_id THEN now() ELSE auth_tokens.created_at END,
	    request_id = GREATEST(auth_tokens.request_id, EXCLUDED.request_id)
	RETURNING ` + authTokenColumns
	args := pgx.NamedArgs{
		"user_id": t.UserID, "request_id": t.RequestID, "purpose": t.Purpose,
		"token_hash": t.TokenHash, "expires_at": t.ExpiresAt,
	}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	token, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.AuthToken])
	if err != nil {
		return db.HandleError(err)
	}
	*t = token
	return nil
}

func (r *authTokenRepo) ConsumeAuthToken(ctx context.Context, tokenHash []byte, purpose string) (*models.AuthToken, error) {
	query := `
	UPDATE auth_tokens
	SET used_at = now()
	WHERE request_id IS NOT NULL AND token_hash = @token_hash AND purpose = @purpose AND used_at IS NULL AND expires_at > now()
	RETURNING ` + authTokenColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"token_hash": tokenHash, "purpose": purpose})
	if err != nil {
		return nil, db.HandleError(err)
	}
	token, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.AuthToken])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &token, nil
}
