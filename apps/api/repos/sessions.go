package repos

//go:generate mockgen -source=sessions.go -destination=repo_mocks/mock_sessions.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/db"
)

// SessionRepo stores login sessions.
type SessionRepo interface {
	// WithTx returns the same repo bound to tx.
	WithTx(tx db.DBTX) SessionRepo
	CreateSession(ctx context.Context, session *models.Session) error
	FindSessionByToken(ctx context.Context, tokenHash []byte) (*models.Session, error)
	RotateRefreshToken(ctx context.Context, session *models.Session, currentHash []byte) error
	RevokeSession(ctx context.Context, id int64) error
	RevokeUserSessions(ctx context.Context, userID int64) error
}

type sessionRepo struct {
	db db.DBTX
}

// NewSessionRepo returns a SessionRepo over a pool or a transaction.
func NewSessionRepo(conn db.DBTX) (SessionRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &sessionRepo{db: conn}, nil
}

func (r *sessionRepo) WithTx(tx db.DBTX) SessionRepo { return &sessionRepo{db: tx} }

const sessionColumns = `id, user_id, token_version, refresh_token_hash, previous_token_hash, expires_at, revoked_at, created_at`

func (r *sessionRepo) CreateSession(ctx context.Context, session *models.Session) error {
	query := `
	INSERT INTO sessions (user_id, token_version, refresh_token_hash, expires_at)
	SELECT id, @token_version, @refresh_token_hash, @expires_at
	FROM users WHERE id = @user_id AND token_version = @token_version
	RETURNING id, created_at
	`
	args := pgx.NamedArgs{
		"user_id":            session.UserID,
		"token_version":      session.TokenVersion,
		"refresh_token_hash": session.RefreshTokenHash,
		"expires_at":         session.ExpiresAt,
	}
	return db.HandleError(r.db.QueryRow(ctx, query, args).Scan(&session.ID, &session.CreatedAt))
}

// FindSessionByToken returns the session whose current or previous refresh token has this hash.
func (r *sessionRepo) FindSessionByToken(ctx context.Context, tokenHash []byte) (*models.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions WHERE refresh_token_hash = @hash OR previous_token_hash = @hash`
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"hash": tokenHash})
	if err != nil {
		return nil, db.HandleError(err)
	}
	session, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.Session])
	if err != nil {
		return nil, db.HandleError(err)
	}
	return &session, nil
}

// RotateRefreshToken replaces currentHash with session.RefreshTokenHash and session.ExpiresAt in one statement.
// It returns apperr.ErrNotFound when currentHash is no longer current or the session is revoked.
func (r *sessionRepo) RotateRefreshToken(ctx context.Context, session *models.Session, currentHash []byte) error {
	query := `
	UPDATE sessions
	SET previous_token_hash = refresh_token_hash,
	    refresh_token_hash  = @new_hash,
	    expires_at          = @expires_at
	WHERE id = @id AND refresh_token_hash = @current_hash AND revoked_at IS NULL
	RETURNING previous_token_hash
	`
	args := pgx.NamedArgs{
		"id":           session.ID,
		"new_hash":     session.RefreshTokenHash,
		"expires_at":   session.ExpiresAt,
		"current_hash": currentHash,
	}
	return db.HandleError(r.db.QueryRow(ctx, query, args).Scan(&session.PreviousTokenHash))
}

func (r *sessionRepo) RevokeSession(ctx context.Context, id int64) error {
	query := `UPDATE sessions SET revoked_at = now() WHERE id = @id AND revoked_at IS NULL`
	_, err := r.db.Exec(ctx, query, pgx.NamedArgs{"id": id})
	return db.HandleError(err)
}

func (r *sessionRepo) RevokeUserSessions(ctx context.Context, userID int64) error {
	query := `UPDATE sessions SET revoked_at = now() WHERE user_id = @user_id AND revoked_at IS NULL`
	_, err := r.db.Exec(ctx, query, pgx.NamedArgs{"user_id": userID})
	return db.HandleError(err)
}
