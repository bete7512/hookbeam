// Package repos implements persistence with hand-written SQL over pgx.
package repos

//go:generate mockgen -source=users.go -destination=repo_mocks/mock_users.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/sorting"
)

// UserRepo reads and writes users.
type UserRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) UserRepo
	CreateUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, id int64) (*models.User, error)
	// GetUserForUpdate locks the user until the enclosing transaction finishes.
	GetUserForUpdate(ctx context.Context, id int64) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	ListUsers(ctx context.Context, opts ListUsersOpts) ([]models.UserForList, error)
	CountUsers(ctx context.Context, opts ListUsersOpts) (int, error)
	UpdateUser(ctx context.Context, user *models.User) error
	UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error
	// ClearUserPassword removes the password so only another login method works, and invalidates access tokens.
	ClearUserPassword(ctx context.Context, id int64) error
	BumpUserTokenVersion(ctx context.Context, id int64) error
	// MarkEmailVerified sets email_verified_at once; a second call is a no-op.
	MarkEmailVerified(ctx context.Context, id int64) error
	DeleteUser(ctx context.Context, id int64) error
}

// ListUsersOpts filters and pages ListUsers and CountUsers.
type ListUsersOpts struct {
	OwnerID int64 // zero permits a trusted internal query across users
	Limit   int
	Offset  int
	Search  string
	SortBy  string
	SortDir string
}

// UserSortColumns are the sort keys clients may use, mapped to columns.
var UserSortColumns = sorting.Columns{
	"id":        "id",
	"email":     "email",
	"name":      "name",
	"createdAt": "created_at",
}

type userRepo struct {
	db db.DBTX
}

// NewUserRepo returns a UserRepo over a pool or a transaction.
func NewUserRepo(conn db.DBTX) (UserRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &userRepo{db: conn}, nil
}

func (r *userRepo) WithTx(tx db.DBTX) UserRepo { return &userRepo{db: tx} }

const userColumns = `id, email, name, password_hash, email_verified_at, token_version, created_at, updated_at`

func (r *userRepo) CreateUser(ctx context.Context, user *models.User) error {
	query := `
	INSERT INTO users (email, name, password_hash)
	VALUES (@email, @name, @password_hash)
	RETURNING id, token_version, created_at, updated_at
	`
	args := pgx.NamedArgs{
		"email":         user.Email,
		"name":          user.Name,
		"password_hash": user.PasswordHash,
	}
	err := r.db.QueryRow(ctx, query, args).Scan(&user.ID, &user.TokenVersion, &user.CreatedAt, &user.UpdatedAt)
	return db.HandleError(err)
}

func (r *userRepo) GetUser(ctx context.Context, id int64) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = @id`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"id": id})
	if err != nil {
		return nil, db.HandleError(err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.User])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &user, nil
}

func (r *userRepo) GetUserForUpdate(ctx context.Context, id int64) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = @id FOR UPDATE`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"id": id})
	if err != nil {
		return nil, db.HandleError(err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.User])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &user, nil
}

func (r *userRepo) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE email = @email`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"email": email})
	if err != nil {
		return nil, db.HandleError(err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.User])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &user, nil
}

const (
	userListColumns = `id, email, name, email_verified_at, created_at, updated_at`
	userListFilter  = `(@owner_id::bigint = 0 OR id = @owner_id) AND (@search::text = '' OR email ILIKE '%' || @search::text || '%' OR name ILIKE '%' || @search::text || '%')`
)

func (r *userRepo) ListUsers(ctx context.Context, opts ListUsersOpts) ([]models.UserForList, error) {
	query := `
	SELECT ` + userListColumns + ` FROM users
	WHERE ` + userListFilter + `
	ORDER BY ` + UserSortColumns.OrderBy(opts.SortBy, opts.SortDir) + `
	LIMIT @limit OFFSET @offset
	`
	args := pgx.NamedArgs{"owner_id": opts.OwnerID, "search": opts.Search, "limit": opts.Limit, "offset": opts.Offset}
	rows, err := r.db.Query(ctx, query, args)
	if err != nil {
		return nil, db.HandleError(err)
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.UserForList])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return users, nil
}

func (r *userRepo) CountUsers(ctx context.Context, opts ListUsersOpts) (int, error) {
	query := `SELECT count(*) FROM users WHERE ` + userListFilter
	var count int
	if err := r.db.QueryRow(ctx, query, pgx.NamedArgs{"owner_id": opts.OwnerID, "search": opts.Search}).Scan(&count); err != nil {
		return 0, db.HandleError(err)
	}
	return count, nil
}

// UpdateUser saves the user's name and refreshes user from the database.
func (r *userRepo) UpdateUser(ctx context.Context, user *models.User) error {
	query := `UPDATE users SET name = @name WHERE id = @id RETURNING ` + userColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"id": user.ID, "name": user.Name})
	if err != nil {
		return db.HandleError(err)
	}
	updated, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.User])
	if err != nil {
		return db.HandleError(err)
	}
	*user = updated
	return nil
}

// UpdateUserPassword sets a new hash and bumps token_version to end existing sessions.
func (r *userRepo) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	query := `UPDATE users SET password_hash = @password_hash, token_version = token_version + 1 WHERE id = @id`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id, "password_hash": passwordHash})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// BumpUserTokenVersion invalidates every access token issued to the user.
func (r *userRepo) ClearUserPassword(ctx context.Context, id int64) error {
	query := `UPDATE users SET password_hash = NULL, token_version = token_version + 1 WHERE id = @id`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *userRepo) MarkEmailVerified(ctx context.Context, id int64) error {
	query := `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = @id`
	tag, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *userRepo) BumpUserTokenVersion(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET token_version = token_version + 1 WHERE id = @id`, pgx.NamedArgs{"id": id})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *userRepo) DeleteUser(ctx context.Context, id int64) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM users WHERE id = @id`, pgx.NamedArgs{"id": id})
	if err != nil {
		return db.HandleError(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.ErrNotFound
	}
	return nil
}
