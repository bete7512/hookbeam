package models

import "time"

// Workspace roles as the specification names them. Owner and admin are goauth's organization roles;
// developer and viewer refine goauth's "member" through the member_roles table (developer is the default).
const (
	RoleOwner     = "owner"
	RoleAdmin     = "admin"
	RoleDeveloper = "developer"
	RoleViewer    = "viewer"
)

// MemberRole is the refinement stored for a goauth member.
type MemberRole struct {
	WorkspaceID string    `db:"workspace_id"`
	UserID      string    `db:"user_id"`
	Role        string    `db:"role"`
	UpdatedBy   string    `db:"updated_by"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// Member is a workspace member with the role Hookbeam applies.
type Member struct {
	UserID   string
	Role     string // owner, admin, developer or viewer
	JoinedAt time.Time
}
