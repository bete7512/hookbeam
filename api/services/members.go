package services

//go:generate mockgen -source=members.go -destination=service_mocks/mock_members.go -package=service_mocks

import (
	"context"
	"log/slog"
	"sort"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/jackc/pgx/v5"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
)

// MemberService lists workspace members with their Hookbeam roles and sets the developer/viewer refinement.
// Owner and admin changes, invitations and removals stay goauth's routes, guarded by OrgPolicy.
type MemberService interface {
	List(ctx context.Context, actor Actor) ([]models.Member, error)
	// SetRole makes a goauth member a developer or a viewer.
	SetRole(ctx context.Context, actor Actor, userID, role string) (*models.Member, error)
}

// MemberServiceDeps are the dependencies; all are required.
type MemberServiceDeps struct {
	Tx          db.Transactor
	Memberships Memberships
	Roles       repos.MemberRoleRepo
	Audit       repos.AuditRepo
	Authz       Authorizer
	Users       Users
	Logger      *slog.Logger
}

type memberService struct {
	deps MemberServiceDeps
}

// NewMemberService returns a MemberService or an error if a dependency is missing.
func NewMemberService(deps MemberServiceDeps) (MemberService, error) {
	if err := depcheck.Required("services", deps); err != nil {
		return nil, err
	}
	return &memberService{deps: deps}, nil
}

func (s *memberService) List(ctx context.Context, actor Actor) ([]models.Member, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapReadMetadata); err != nil {
		return nil, err
	}
	members, err := s.deps.Memberships.Members(ctx, actor.WorkspaceID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	refined, err := s.deps.Roles.ListMemberRoles(ctx, actor.WorkspaceID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	for i := range members {
		if members[i].Role == "member" {
			if r, ok := refined[members[i].UserID]; ok {
				members[i].Role = r
			} else {
				members[i].Role = models.RoleDeveloper
			}
		}
		// The person behind the membership is goauth's; a user that vanished leaves the row with its id only.
		if c, err := s.deps.Users.Contact(ctx, members[i].UserID); err == nil {
			members[i].Name, members[i].Email = c.Name, c.Email
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].JoinedAt.Before(members[j].JoinedAt) })
	return members, nil
}

func (s *memberService) SetRole(ctx context.Context, actor Actor, userID, role string) (*models.Member, error) {
	if err := s.deps.Authz.Can(ctx, actor, CapManageMembers); err != nil {
		return nil, err
	}
	if role != models.RoleDeveloper && role != models.RoleViewer {
		return nil, apperr.NewValidation(map[string]string{"role": "must be developer or viewer; owner and admin are set through the organization routes"})
	}
	raw, err := s.deps.Memberships.Role(ctx, actor.WorkspaceID, userID)
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	switch raw {
	case "":
		return nil, apperr.NewNotFound("member", nil)
	case "member":
	default:
		return nil, apperr.NewConflict("an "+raw+" keeps that role; change it through the organization routes first", nil)
	}
	err = s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.deps.Roles.WithTx(tx).SetMemberRole(ctx, &models.MemberRole{
			WorkspaceID: actor.WorkspaceID,
			UserID:      userID,
			Role:        role,
			UpdatedBy:   actor.UserID,
		}); err != nil {
			return err
		}
		return recordAudit(ctx, s.deps.Audit.WithTx(tx), actor, AuditMemberRole, "member", 0, map[string]any{
			"user_id": userID,
			"role":    role,
		})
	})
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return &models.Member{UserID: userID, Role: role}, nil
}
