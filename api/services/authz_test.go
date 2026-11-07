package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// fakeMemberships answers goauth roles from a map; a missing user is not a member.
type fakeMemberships struct {
	roles map[string]string
	err   error
}

func (f fakeMemberships) Role(_ context.Context, _, userID string) (string, error) {
	return f.roles[userID], f.err
}

func (f fakeMemberships) Members(_ context.Context, _ string) ([]models.Member, error) {
	out := make([]models.Member, 0, len(f.roles))
	for id, role := range f.roles {
		out = append(out, models.Member{UserID: id, Role: role})
	}
	return out, f.err
}

func newAuthorizer(t *testing.T, members *fakeMemberships) (services.Authorizer, *repo_mocks.MockMemberRoleRepo) {
	t.Helper()
	roles := repo_mocks.NewMockMemberRoleRepo(gomock.NewController(t))
	authz, err := services.NewAuthorizer(services.AuthorizerDeps{Memberships: members, Roles: roles, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return authz, roles
}

func TestNewAuthorizer(t *testing.T) {
	_, err := services.NewAuthorizer(services.AuthorizerDeps{})
	require.Error(t, err)
}

// TestRoleMatrix pins every row of the specification's capability table for every role.
func TestRoleMatrix(t *testing.T) {
	allow := map[services.Capability][]string{
		services.CapReadMetadata:    {"owner", "admin", "developer", "viewer"},
		services.CapReadPayloads:    {"owner", "admin", "developer"},
		services.CapReplay:          {"owner", "admin", "developer"},
		services.CapManageEndpoints: {"owner", "admin"},
		services.CapManageKeys:      {"owner", "admin"},
		services.CapManageMembers:   {"owner", "admin"},
		services.CapManageOwners:    {"owner"},
	}
	for c, roles := range allow {
		for _, role := range []string{"owner", "admin", "developer", "viewer"} {
			want := false
			for _, r := range roles {
				want = want || r == role
			}
			require.Equal(t, want, services.RoleAllows(role, c), "%s / %s", role, c)
		}
	}
	require.False(t, services.RoleAllows("", services.CapReadMetadata), "a non-member holds nothing")
}

func TestAuthorizerRoleAndCan(t *testing.T) {
	members := &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "member", "u_viewer": "member"}}
	tests := []struct {
		name     string
		user     string
		stored   func(roles *repo_mocks.MockMemberRoleRepo)
		wantRole string
		can      services.Capability
		wantErr  string
	}{
		{name: "owner", user: "u_owner", wantRole: "owner", can: services.CapManageOwners},
		{name: "admin cannot appoint owners", user: "u_admin", wantRole: "admin", can: services.CapManageOwners, wantErr: "a admin cannot appoint or remove owners"},
		{name: "member defaults to developer", user: "u_dev", stored: func(r *repo_mocks.MockMemberRoleRepo) {
			r.EXPECT().GetMemberRole(gomock.Any(), "ws_1", "u_dev").Return("", apperr.ErrNotFound).Times(2)
		}, wantRole: "developer", can: services.CapReplay},
		{name: "stored viewer cannot replay", user: "u_viewer", stored: func(r *repo_mocks.MockMemberRoleRepo) {
			r.EXPECT().GetMemberRole(gomock.Any(), "ws_1", "u_viewer").Return("viewer", nil).Times(2)
		}, wantRole: "viewer", can: services.CapReplay, wantErr: "a viewer cannot submit test events and replay or cancel deliveries"},
		{name: "not a member", user: "u_stranger", wantRole: "", can: services.CapReadMetadata, wantErr: "not a member of this workspace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authz, roles := newAuthorizer(t, members)
			if tt.stored != nil {
				tt.stored(roles)
			}
			actor := services.Actor{UserID: tt.user, WorkspaceID: "ws_1"}

			role, err := authz.Role(context.Background(), actor)
			require.NoError(t, err)
			require.Equal(t, tt.wantRole, role)

			err = authz.Can(context.Background(), actor, tt.can)
			if tt.wantErr != "" {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, 403, appErr.Code)
				require.Equal(t, tt.wantErr, appErr.Message)
				return
			}
			require.NoError(t, err)
		})
	}
	t.Run("no session or workspace", func(t *testing.T) {
		authz, _ := newAuthorizer(t, members)
		require.ErrorIs(t, authz.Can(context.Background(), services.Actor{WorkspaceID: "ws_1"}, services.CapReadMetadata), apperr.ErrForbidden)
		require.ErrorIs(t, authz.Can(context.Background(), services.Actor{UserID: "u_owner"}, services.CapReadMetadata), apperr.ErrForbidden)
	})
	t.Run("membership lookup fails", func(t *testing.T) {
		authz, _ := newAuthorizer(t, &fakeMemberships{err: errors.New("boom")})
		require.ErrorIs(t, authz.Can(context.Background(), services.Actor{UserID: "u", WorkspaceID: "ws_1"}, services.CapReadMetadata), apperr.ErrInternal)
	})
}
