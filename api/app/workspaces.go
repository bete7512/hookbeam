package app

import (
	"context"
	"errors"

	goauthmodels "github.com/bete7512/goauth/pkg/models"
	"github.com/bete7512/goauth/pkg/types"

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
