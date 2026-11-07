package repos

//go:generate mockgen -source=user_identities.go -destination=repo_mocks/mock_user_identities.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/db"
)

// UserIdentityRepo stores links between users and external identity providers.
type UserIdentityRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) UserIdentityRepo
	CreateUserIdentity(ctx context.Context, id *models.UserIdentity) error
	GetUserIdentity(ctx context.Context, provider, subject string) (*models.UserIdentity, error)
}

type userIdentityRepo struct {
	db db.DBTX
}

// NewUserIdentityRepo returns a UserIdentityRepo over a pool or a transaction.
func NewUserIdentityRepo(conn db.DBTX) (UserIdentityRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &userIdentityRepo{db: conn}, nil
}

func (r *userIdentityRepo) WithTx(tx db.DBTX) UserIdentityRepo { return &userIdentityRepo{db: tx} }

const userIdentityColumns = `id, user_id, provider, subject, email, created_at`

func (r *userIdentityRepo) CreateUserIdentity(ctx context.Context, id *models.UserIdentity) error {
	query := `
	INSERT INTO user_identities (user_id, provider, subject, email)
	VALUES (@user_id, @provider, @subject, @email)
	RETURNING id, created_at
	`
	args := pgx.NamedArgs{
		"user_id":  id.UserID,
		"provider": id.Provider,
		"subject":  id.Subject,
		"email":    id.Email,
	}
	return db.HandleError(r.db.QueryRow(ctx, query, args).Scan(&id.ID, &id.CreatedAt))
}

func (r *userIdentityRepo) GetUserIdentity(ctx context.Context, provider, subject string) (*models.UserIdentity, error) {
	query := `SELECT ` + userIdentityColumns + ` FROM user_identities WHERE provider = @provider AND subject = @subject`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"provider": provider, "subject": subject})
	if err != nil {
		return nil, db.HandleError(err)
	}
	identity, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.UserIdentity])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &identity, nil
}
