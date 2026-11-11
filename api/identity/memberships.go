package identity

import (
	"context"
	"errors"

	goauthmodels "github.com/bete7512/goauth/pkg/models"
	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
)

// memberPage is how many members one storage call returns.
const memberPage = 200

// memberships answers workspace roles from goauth's organization storage.
type memberships struct {
	store types.OrganizationStorage
}

// NewMemberships returns the Memberships port over goauth's organization storage.
func NewMemberships(store types.OrganizationStorage) (services.Memberships, error) {
	if store == nil {
		return nil, errors.New("identity: organization storage is required")
	}
	return memberships{store: store}, nil
}

// Role returns the user's role in the workspace, or "" when not a member.
func (m memberships) Role(ctx context.Context, workspaceID, userID string) (string, error) {
	member, err := m.store.Members().FindByOrgAndUser(ctx, workspaceID, userID)
	if errors.Is(err, goauthmodels.ErrNotFound) || (err == nil && member == nil) {
		return "", nil
	}
	if err != nil {
		return "", err
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
