package services

//go:generate mockgen -source=authz.go -destination=service_mocks/mock_authz.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// Capability is one row of the specification's role matrix.
type Capability string

// The capabilities, worded as the forbidden message reads them ("a viewer cannot ...").
const (
	CapReadMetadata    Capability = "read delivery metadata"
	CapReadPayloads    Capability = "read payloads and response previews"
	CapReplay          Capability = "submit test events and replay or cancel deliveries"
	CapManageEndpoints Capability = "manage applications, endpoints and signing secrets"
	CapManageKeys      Capability = "create and revoke api keys"
	CapManageMembers   Capability = "manage members and invitations"
	CapManageOwners    Capability = "appoint or remove owners"
)

// matrix is the specification's table: which roles hold each capability.
var matrix = map[Capability]map[string]bool{
	CapReadMetadata: {
		models.RoleOwner:     true,
		models.RoleAdmin:     true,
		models.RoleDeveloper: true,
		models.RoleViewer:    true,
	},
	CapReadPayloads:    {models.RoleOwner: true, models.RoleAdmin: true, models.RoleDeveloper: true},
	CapReplay:          {models.RoleOwner: true, models.RoleAdmin: true, models.RoleDeveloper: true},
	CapManageEndpoints: {models.RoleOwner: true, models.RoleAdmin: true},
	CapManageKeys:      {models.RoleOwner: true, models.RoleAdmin: true},
	CapManageMembers:   {models.RoleOwner: true, models.RoleAdmin: true},
	CapManageOwners:    {models.RoleOwner: true},
}

// RoleAllows reports whether a role holds the capability.
func RoleAllows(role string, c Capability) bool { return matrix[c][role] }

// Authorizer resolves a user's workspace role and answers the role matrix.
type Authorizer interface {
	// Role returns the actor's effective role in the workspace (owner, admin, developer, viewer) or "" when not a member.
	Role(ctx context.Context, actor Actor) (string, error)
	// Can returns a forbidden error unless the actor's role holds the capability.
	Can(ctx context.Context, actor Actor, c Capability) error
}

// AuthorizerDeps are the dependencies; all are required.
type AuthorizerDeps struct {
	Memberships Memberships
	Roles       repos.MemberRoleRepo
	Logger      *slog.Logger
}

type authorizer struct {
	deps AuthorizerDeps
}

// NewAuthorizer returns an Authorizer or an error if a dependency is missing.
func NewAuthorizer(deps AuthorizerDeps) (Authorizer, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &authorizer{deps: deps}, nil
}

func (a *authorizer) Role(ctx context.Context, actor Actor) (string, error) {
	if actor.UserID == "" || actor.WorkspaceID == "" {
		return "", nil
	}
	raw, err := a.deps.Memberships.Role(ctx, actor.WorkspaceID, actor.UserID)
	if err != nil {
		return "", apperr.NewInternal(err)
	}
	return a.effective(ctx, actor.WorkspaceID, actor.UserID, raw)
}

// effective maps goauth's role to Hookbeam's: owner and admin as they are, member to the stored refinement
// or developer by default.
func (a *authorizer) effective(ctx context.Context, workspaceID, userID, raw string) (string, error) {
	switch raw {
	case models.RoleOwner, models.RoleAdmin:
		return raw, nil
	case "member":
		role, err := a.deps.Roles.GetMemberRole(ctx, workspaceID, userID)
		if errors.Is(err, apperr.ErrNotFound) {
			return models.RoleDeveloper, nil
		}
		if err != nil {
			return "", apperr.NewInternal(err)
		}
		return role, nil
	}
	return "", nil
}

func (a *authorizer) Can(ctx context.Context, actor Actor, c Capability) error {
	if actor.UserID == "" {
		return apperr.NewForbidden("a user session is required", nil)
	}
	if actor.WorkspaceID == "" {
		return apperr.NewForbidden("select a workspace", nil)
	}
	role, err := a.Role(ctx, actor)
	if err != nil {
		return err
	}
	if role == "" {
		return apperr.NewForbidden("not a member of this workspace", nil)
	}
	if !RoleAllows(role, c) {
		return apperr.NewForbidden("a "+role+" cannot "+string(c), nil)
	}
	return nil
}
