package repos

//go:generate mockgen -source=api_keys.go -destination=repo_mocks/mock_api_keys.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/sorting"
)

// APIKeyRepo reads and writes sender API keys.
type APIKeyRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) APIKeyRepo
	CreateAPIKey(ctx context.Context, key *models.APIKey) error
	// FindActiveAPIKeyByHash returns the key unless it is revoked, expired or unknown.
	FindActiveAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOpts) ([]models.APIKeyForList, error)
	CountAPIKeys(ctx context.Context, opts ListAPIKeysOpts) (int, error)
	// RevokeAPIKey revokes an active key of the workspace; a revoked or foreign key is not found.
	RevokeAPIKey(ctx context.Context, workspaceID string, id int64) error
	// TouchAPIKey records a use, at most once a minute per key.
	TouchAPIKey(ctx context.Context, id int64) error
}

// ListAPIKeysOpts filters and pages ListAPIKeys and CountAPIKeys within one workspace.
type ListAPIKeysOpts struct {
	WorkspaceID string
	Limit       int
	Offset      int
	Search      string
	SortBy      string
	SortDir     string
}

// APIKeySortColumns are the sort keys clients may use, mapped to columns.
var APIKeySortColumns = sorting.Columns{
	"id":         "id",
	"name":       "name",
	"createdAt":  "created_at",
	"lastUsedAt": "last_used_at",
}

type apiKeyRepo struct {
	db db.DBTX
}

// NewAPIKeyRepo returns an APIKeyRepo over a pool or a transaction.
func NewAPIKeyRepo(conn db.DBTX) (APIKeyRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &apiKeyRepo{db: conn}, nil
}

func (r *apiKeyRepo) WithTx(tx db.DBTX) APIKeyRepo { return &apiKeyRepo{db: tx} }

//nolint:gosec // column lists and a filter, not credentials
const (
	apiKeyColumns     = `id, workspace_id, name, prefix, key_hash, scopes, application_id, created_by, expires_at, last_used_at, revoked_at, created_at, updated_at`
	apiKeyListColumns = `id, workspace_id, name, prefix, scopes, application_id, created_by, expires_at, last_used_at, revoked_at, created_at, updated_at`
	apiKeyListFilter  = `workspace_id = @workspace_id AND (@search::text = '' OR name ILIKE '%' || @search::text || '%')`
)

func (r *apiKeyRepo) CreateAPIKey(ctx context.Context, key *models.APIKey) error {
	query := `
	INSERT INTO api_keys (workspace_id, name, prefix, key_hash, scopes, application_id, created_by, expires_at)
	VALUES (@workspace_id, @name, @prefix, @key_hash, @scopes, @application_id, @created_by, @expires_at)
	RETURNING id, created_at, updated_at
	`
	args := pgx.NamedArgs{
		"workspace_id":   key.WorkspaceID,
		"name":           key.Name,
		"prefix":         key.Prefix,
		"key_hash":       key.KeyHash,
		"scopes":         key.Scopes,
		"application_id": key.ApplicationID,
		"created_by":     key.CreatedBy,
		"expires_at":     key.ExpiresAt,
	}
	err := r.db.QueryRow(ctx, query, args).Scan(&key.ID, &key.CreatedAt, &key.UpdatedAt)
	return db.HandleError(err)
}

func (r *apiKeyRepo) FindActiveAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	query := `
	SELECT ` + apiKeyColumns + ` FROM api_keys
	WHERE key_hash = @key_hash AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
	`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"key_hash": hash})
	if err != nil {
		return nil, db.HandleError(err)
	}
	key, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.APIKey])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &key, nil
}

func (r *apiKeyRepo) ListAPIKeys(ctx context.Context, opts ListAPIKeysOpts) ([]models.APIKeyForList, error) {
	query := `
	SELECT ` + apiKeyListColumns + ` FROM api_keys
	WHERE ` + apiKeyListFilter + `
	ORDER BY ` + APIKeySortColumns.OrderBy(opts.SortBy, opts.SortDir) + `
	LIMIT @limit OFFSET @offset
	`
	args := pgx.NamedArgs{"workspace_id": opts.WorkspaceID, "search": opts.Search, "limit": opts.Limit, "offset": opts.Offset}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.APIKeyForList])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return keys, nil
}

func (r *apiKeyRepo) CountAPIKeys(ctx context.Context, opts ListAPIKeysOpts) (int, error) {
	query := `SELECT count(*) FROM api_keys WHERE ` + apiKeyListFilter
	var total int
	err := r.db.QueryRow(ctx, query, pgx.NamedArgs{"workspace_id": opts.WorkspaceID, "search": opts.Search}).Scan(&total)
	return total, db.HandleError(err)
}

func (r *apiKeyRepo) RevokeAPIKey(ctx context.Context, workspaceID string, id int64) error {
	query := `UPDATE api_keys SET revoked_at = now() WHERE id = @id AND workspace_id = @workspace_id AND revoked_at IS NULL`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id, "workspace_id": workspaceID})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *apiKeyRepo) TouchAPIKey(ctx context.Context, id int64) error {
	query := `UPDATE api_keys SET last_used_at = now() WHERE id = @id AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`
	_, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id})
	return db.HandleError(err)
}
