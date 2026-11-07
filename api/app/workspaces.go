package app

import (
	"context"
	"errors"

	goauthmodels "github.com/bete7512/goauth/pkg/models"
	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
)

// memberships answers workspace roles from goauth's organization storage.
type memberships struct {
	store types.OrganizationStorage
}

var _ services.Memberships = memberships{}

// Role returns the user's role in the workspace, or "" when not a member.
func (m memberships) Role(ctx context.Context, workspaceID, userID string) (string, error) {
	member, err := m.store.Members().FindByOrgAndUser(ctx, workspaceID, userID)
	if errors.Is(err, goauthmodels.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if member == nil {
		return "", nil
	}
	return member.Role, nil
}

// Members returns every member of the workspace with goauth's role.
func (m memberships) Members(ctx context.Context, workspaceID string) ([]models.Member, error) {
	var out []models.Member
	for offset := 0; ; offset += memberPage {
		opts := goauthmodels.MemberListOpts{ListingOpts: goauthmodels.ListingOpts{Offset: offset, Limit: memberPage, SortField: "joined_at", SortDir: "asc"}}
		opts.Normalize(memberPage)
		page, _, err := m.store.Members().ListByOrg(ctx, workspaceID, opts)
		if err != nil {
			return nil, err
		}
		for _, member := range page {
			out = append(out, models.Member{UserID: member.UserID, Role: member.Role, JoinedAt: member.JoinedAt})
		}
		if len(page) < memberPage {
			return out, nil
		}
	}
}

// memberPage is how many members one storage call returns.
const memberPage = 200
