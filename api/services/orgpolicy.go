package services

//go:generate mockgen -source=orgpolicy.go -destination=service_mocks/mock_orgpolicy.go -package=service_mocks

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// OrgAction is a goauth organization route Hookbeam guards.
type OrgAction string

// The guarded actions.
const (
	OrgInvite       OrgAction = "invite"
	OrgChangeRole   OrgAction = "change role"
	OrgRemoveMember OrgAction = "remove member"
)

// OrgTarget is who an action is about and the goauth role it asks for (invite and change role).
type OrgTarget struct {
	UserID string
	Email  string
	Role   string // goauth role: owner, admin, member; "" means member
}

// OrgPolicy enforces the specification's membership rules in front of goauth's routes: nobody grants a role
// above their own, only owners touch owners, and the workspace always keeps an owner.
type OrgPolicy interface {
	Check(ctx context.Context, actor Actor, action OrgAction, target OrgTarget) error
}

// OrgPolicyDeps are the dependencies; all are required.
type OrgPolicyDeps struct {
	Memberships Memberships
	Authz       Authorizer
	Logger      *slog.Logger
}

type orgPolicy struct {
	deps OrgPolicyDeps
}

// NewOrgPolicy returns an OrgPolicy or an error if a dependency is missing.
func NewOrgPolicy(deps OrgPolicyDeps) (OrgPolicy, error) {
	if deps.Memberships == nil || deps.Authz == nil || deps.Logger == nil {
		return nil, errors.New("services: Memberships, Authz and Logger are required")
	}
	return &orgPolicy{deps: deps}, nil
}

var goauthRank = map[string]int{"member": 1, models.RoleAdmin: 2, models.RoleOwner: 3}

func (p *orgPolicy) Check(ctx context.Context, actor Actor, action OrgAction, target OrgTarget) error {
	if err := p.deps.Authz.Can(ctx, actor, CapManageMembers); err != nil {
		return err
	}
	actorRole, err := p.deps.Memberships.Role(ctx, actor.WorkspaceID, actor.UserID)
	if err != nil {
		return apperr.NewInternal(err)
	}
	requested := target.Role
	if requested == "" {
		requested = "member"
	}
	if _, ok := goauthRank[requested]; !ok {
		return apperr.NewValidation(map[string]string{"role": "must be owner, admin or member"})
	}
	switch action {
	case OrgInvite:
		if goauthRank[requested] > goauthRank[actorRole] {
			return apperr.NewForbidden("an "+actorRole+" cannot invite an "+requested, nil)
		}
		if requested == models.RoleOwner && actorRole != models.RoleOwner {
			return apperr.NewForbidden("only an owner can invite an owner", nil)
		}
		return nil
	case OrgChangeRole, OrgRemoveMember:
		targetRole, err := p.deps.Memberships.Role(ctx, actor.WorkspaceID, target.UserID)
		if err != nil {
			return apperr.NewInternal(err)
		}
		if targetRole == "" {
			return apperr.NewNotFound("member", nil)
		}
		if (targetRole == models.RoleOwner || action == OrgChangeRole && requested == models.RoleOwner) && actorRole != models.RoleOwner {
			return apperr.NewForbidden("only an owner can change or remove an owner", nil)
		}
		if action == OrgChangeRole && goauthRank[requested] > goauthRank[actorRole] {
			return apperr.NewForbidden("an "+actorRole+" cannot grant the "+requested+" role", nil)
		}
		losesOwner := targetRole == models.RoleOwner && (action == OrgRemoveMember || requested != models.RoleOwner)
		if losesOwner {
			n, err := p.owners(ctx, actor.WorkspaceID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apperr.NewForbidden("the workspace must keep at least one owner", nil)
			}
		}
		return nil
	}
	return apperr.NewBadRequest("unknown organization action", nil)
}

func (p *orgPolicy) owners(ctx context.Context, workspaceID string) (int, error) {
	members, err := p.deps.Memberships.Members(ctx, workspaceID)
	if err != nil {
		return 0, apperr.NewInternal(err)
	}
	n := 0
	for _, m := range members {
		if m.Role == models.RoleOwner {
			n++
		}
	}
	return n, nil
}
