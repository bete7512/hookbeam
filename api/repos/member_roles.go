package repos

//go:generate mockgen -source=member_roles.go -destination=repo_mocks/mock_member_roles.go -package=repo_mocks

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/db"
)

// MemberRoleRepo stores the developer/viewer refinement of goauth members.
type MemberRoleRepo interface {
	// GetMemberRole returns the stored role, or ErrNotFound when the member has the default.
	GetMemberRole(ctx context.Context, workspaceID, userID string) (string, error)
	// ListMemberRoles returns every stored role of the workspace by user id.
	ListMemberRoles(ctx context.Context, workspaceID string) (map[string]string, error)
	// SetMemberRole stores or replaces the role.
	SetMemberRole(ctx context.Context, r *models.MemberRole) error
	// DeleteMemberRole removes the refinement; a missing row is not an error.
	DeleteMemberRole(ctx context.Context, workspaceID, userID string) error
}

type memberRoleRepo struct {
	db db.DBTX
}

// NewMemberRoleRepo returns a MemberRoleRepo over a pool or a transaction.
func NewMemberRoleRepo(conn db.DBTX) (MemberRoleRepo, error) {
	if conn == nil {
		return nil, errors.New("repos: db is required")
	}
	return &memberRoleRepo{db: conn}, nil
}

const memberRoleColumns = `workspace_id, user_id, role, updated_by, created_at, updated_at`

func (r *memberRoleRepo) GetMemberRole(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := r.db.QueryRow(ctx, `SELECT role FROM member_roles WHERE workspace_id = @workspace_id AND user_id = @user_id`, pgx.NamedArgs{"workspace_id": workspaceID, "user_id": userID}).Scan(&role)
	if err != nil {
		return "", db.HandleError(err)
	}
	return role, nil
}

func (r *memberRoleRepo) ListMemberRoles(ctx context.Context, workspaceID string) (map[string]string, error) {
	rows, err := r.db.Query(ctx, `SELECT `+memberRoleColumns+` FROM member_roles WHERE workspace_id = @workspace_id`, pgx.NamedArgs{"workspace_id": workspaceID})
	if err != nil {
		return nil, db.HandleError(err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.MemberRole])
	if err != nil {
		return nil, db.HandleError(err)
	}
	out := make(map[string]string, len(list))
	for _, m := range list {
		out[m.UserID] = m.Role
	}
	return out, nil
}

func (r *memberRoleRepo) SetMemberRole(ctx context.Context, m *models.MemberRole) error {
	query := `
	INSERT INTO member_roles (workspace_id, user_id, role, updated_by) VALUES (@workspace_id, @user_id, @role, @updated_by)
	ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role, updated_by = EXCLUDED.updated_by
	RETURNING ` + memberRoleColumns
	rows, err := r.db.Query(ctx, query, pgx.NamedArgs{"workspace_id": m.WorkspaceID, "user_id": m.UserID, "role": m.Role, "updated_by": m.UpdatedBy})
	if err != nil {
		return db.HandleError(err)
	}
	stored, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[models.MemberRole])
	if err != nil {
		return db.HandleError(err)
	}
	*m = stored
	return nil
}

func (r *memberRoleRepo) DeleteMemberRole(ctx context.Context, workspaceID, userID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM member_roles WHERE workspace_id = @workspace_id AND user_id = @user_id`, pgx.NamedArgs{"workspace_id": workspaceID, "user_id": userID})
	return db.HandleError(err)
}
