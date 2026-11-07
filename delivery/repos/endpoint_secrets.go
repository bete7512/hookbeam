package repos

//go:generate mockgen -source=endpoint_secrets.go -destination=repo_mocks/mock_endpoint_secrets.go -package=repo_mocks

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// EndpointSecretRepo stores the encrypted signing secrets of endpoints.
type EndpointSecretRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) EndpointSecretRepo
	// CreateEndpointSecret inserts the next version for the endpoint and fills Version.
	CreateEndpointSecret(ctx context.Context, s *models.EndpointSecret) error
	// RetireEndpointSecrets sets retire_at on the endpoint's other active secrets (an earlier retire_at is kept)
	// and returns how many it touched.
	RetireEndpointSecrets(ctx context.Context, endpointID int64, keepVersion int32, retireAt time.Time) (int64, error)
	// ListActiveEndpointSecrets returns the secrets that still sign, newest version first.
	ListActiveEndpointSecrets(ctx context.Context, endpointID int64) ([]models.EndpointSecret, error)
}

type endpointSecretRepo struct {
	db db.DBTX
}

// NewEndpointSecretRepo returns an EndpointSecretRepo over a pool or a transaction.
func NewEndpointSecretRepo(conn db.DBTX) (EndpointSecretRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &endpointSecretRepo{db: conn}, nil
}

func (r *endpointSecretRepo) WithTx(tx db.DBTX) EndpointSecretRepo {
	return &endpointSecretRepo{db: tx}
}

const endpointSecretColumns = `id, endpoint_id, version, ciphertext, key_id, fingerprint, created_at, retire_at` //nolint:gosec // column list, not a credential

func (r *endpointSecretRepo) CreateEndpointSecret(ctx context.Context, s *models.EndpointSecret) error {
	query := `
	INSERT INTO endpoint_secrets (endpoint_id, version, ciphertext, key_id, fingerprint)
	VALUES (@endpoint_id, (SELECT COALESCE(MAX(version), 0) + 1 FROM endpoint_secrets WHERE endpoint_id = @endpoint_id), @ciphertext, @key_id, @fingerprint)
	RETURNING ` + endpointSecretColumns
	args := pgx.NamedArgs{"endpoint_id": s.EndpointID, "ciphertext": s.Ciphertext, "key_id": s.KeyID, "fingerprint": s.Fingerprint}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return db.HandleError(err)
	}
	created, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.EndpointSecret])
	if err != nil {
		return db.HandleError(err)
	}
	*s = created
	return nil
}

func (r *endpointSecretRepo) RetireEndpointSecrets(ctx context.Context, endpointID int64, keepVersion int32, retireAt time.Time) (int64, error) {
	query := `
	UPDATE endpoint_secrets SET retire_at = @retire_at
	WHERE endpoint_id = @endpoint_id AND version <> @keep AND (retire_at IS NULL OR retire_at > @retire_at)`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"endpoint_id": endpointID, "keep": keepVersion, "retire_at": retireAt})
	if err != nil {
		return 0, db.HandleError(err)
	}
	return tag.RowsAffected(), nil
}

func (r *endpointSecretRepo) ListActiveEndpointSecrets(ctx context.Context, endpointID int64) ([]models.EndpointSecret, error) {
	query := `SELECT ` + endpointSecretColumns + ` FROM endpoint_secrets WHERE endpoint_id = @endpoint_id AND (retire_at IS NULL OR retire_at > now()) ORDER BY version DESC`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"endpoint_id": endpointID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.EndpointSecret])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return out, nil
}
