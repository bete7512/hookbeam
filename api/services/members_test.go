package services_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type memberFixture struct {
	members *fakeMemberships
	roles   *repo_mocks.MockMemberRoleRepo
	authz   *service_mocks.MockAuthorizer
	svc     services.MemberService
}

func newMembers(t *testing.T) *memberFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &memberFixture{
		members: &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "member"}},
		roles:   repo_mocks.NewMockMemberRoleRepo(ctrl),
		authz:   service_mocks.NewMockAuthorizer(ctrl),
	}
	var err error
	f.svc, err = services.NewMemberService(services.MemberServiceDeps{Memberships: f.members, Roles: f.roles, Authz: f.authz, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	return f
}

func TestMemberServiceList(t *testing.T) {
	actor := services.Actor{UserID: "u_admin", WorkspaceID: "ws_1"}
	t.Run("members carry the roles Hookbeam applies", func(t *testing.T) {
		f := newMembers(t)
		f.authz.EXPECT().Can(gomock.Any(), actor, services.CapReadMetadata).Return(nil)
		f.roles.EXPECT().ListMemberRoles(gomock.Any(), "ws_1").Return(map[string]string{"u_dev": "viewer"}, nil)
		got, err := f.svc.List(context.Background(), actor)
		require.NoError(t, err)
		byUser := map[string]string{}
		for _, m := range got {
			byUser[m.UserID] = m.Role
		}
		require.Equal(t, map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "viewer"}, byUser)
	})
	t.Run("forbidden by the matrix", func(t *testing.T) {
		f := newMembers(t)
		f.authz.EXPECT().Can(gomock.Any(), actor, services.CapReadMetadata).Return(apperr.NewForbidden("no", nil))
		_, err := f.svc.List(context.Background(), actor)
		require.ErrorIs(t, err, apperr.ErrForbidden)
	})
}

func TestMemberServiceSetRole(t *testing.T) {
	actor := services.Actor{UserID: "u_admin", WorkspaceID: "ws_1"}
	tests := []struct {
		name       string
		user, role string
		setup      func(f *memberFixture)
		wantStatus int
	}{
		{name: "member becomes viewer", user: "u_dev", role: "viewer", setup: func(f *memberFixture) {
			f.authz.EXPECT().Can(gomock.Any(), actor, services.CapManageMembers).Return(nil)
			f.roles.EXPECT().SetMemberRole(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *models.MemberRole) error {
				require.Equal(t, "u_admin", m.UpdatedBy)
				require.Equal(t, "viewer", m.Role)
				return nil
			})
		}},
		{name: "owner is refused with a pointer to the organization routes", user: "u_owner", role: "viewer", setup: func(f *memberFixture) {
			f.authz.EXPECT().Can(gomock.Any(), actor, services.CapManageMembers).Return(nil)
		}, wantStatus: 409},
		{name: "unknown user", user: "u_nobody", role: "viewer", setup: func(f *memberFixture) {
			f.authz.EXPECT().Can(gomock.Any(), actor, services.CapManageMembers).Return(nil)
		}, wantStatus: 404},
		{name: "owner and admin are not valid here", user: "u_dev", role: "admin", setup: func(f *memberFixture) {
			f.authz.EXPECT().Can(gomock.Any(), actor, services.CapManageMembers).Return(nil)
		}, wantStatus: 422},
		{name: "a developer cannot manage members", user: "u_dev", role: "viewer", setup: func(f *memberFixture) {
			f.authz.EXPECT().Can(gomock.Any(), actor, services.CapManageMembers).Return(apperr.NewForbidden("no", nil))
		}, wantStatus: 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newMembers(t)
			tt.setup(f)
			got, err := f.svc.SetRole(context.Background(), actor, tt.user, tt.role)
			if tt.wantStatus != 0 {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, tt.wantStatus, appErr.Code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.role, got.Role)
		})
	}
}

func TestOrgPolicy(t *testing.T) {
	members := &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_owner2": "owner", "u_admin": "admin", "u_dev": "member"}}
	soleOwner := &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "member"}}
	tests := []struct {
		name    string
		members *fakeMemberships
		actor   string
		action  services.OrgAction
		target  services.OrgTarget
		wantErr string
	}{
		{name: "admin invites a member", members: members, actor: "u_admin", action: services.OrgInvite, target: services.OrgTarget{Email: "x@example.com"}},
		{name: "admin invites an admin", members: members, actor: "u_admin", action: services.OrgInvite, target: services.OrgTarget{Role: "admin"}},
		{name: "admin cannot invite an owner", members: members, actor: "u_admin", action: services.OrgInvite, target: services.OrgTarget{Role: "owner"}, wantErr: "an admin cannot invite an owner"},
		{name: "owner invites an owner", members: members, actor: "u_owner", action: services.OrgInvite, target: services.OrgTarget{Role: "owner"}},
		{name: "unknown role", members: members, actor: "u_owner", action: services.OrgInvite, target: services.OrgTarget{Role: "god"}, wantErr: "validation failed"},
		{name: "admin promotes a member to admin", members: members, actor: "u_admin", action: services.OrgChangeRole, target: services.OrgTarget{UserID: "u_dev", Role: "admin"}},
		{name: "admin cannot promote to owner", members: members, actor: "u_admin", action: services.OrgChangeRole, target: services.OrgTarget{UserID: "u_dev", Role: "owner"}, wantErr: "only an owner can change or remove an owner"},
		{name: "admin cannot demote an owner", members: members, actor: "u_admin", action: services.OrgChangeRole, target: services.OrgTarget{UserID: "u_owner", Role: "admin"}, wantErr: "only an owner can change or remove an owner"},
		{name: "owner demotes another owner while one remains", members: members, actor: "u_owner", action: services.OrgChangeRole, target: services.OrgTarget{UserID: "u_owner2", Role: "admin"}},
		{name: "the last owner cannot be demoted", members: soleOwner, actor: "u_owner", action: services.OrgChangeRole, target: services.OrgTarget{UserID: "u_owner", Role: "admin"}, wantErr: "the workspace must keep at least one owner"},
		{name: "the last owner cannot be removed", members: soleOwner, actor: "u_owner", action: services.OrgRemoveMember, target: services.OrgTarget{UserID: "u_owner"}, wantErr: "the workspace must keep at least one owner"},
		{name: "admin removes a member", members: members, actor: "u_admin", action: services.OrgRemoveMember, target: services.OrgTarget{UserID: "u_dev"}},
		{name: "admin cannot remove an owner", members: members, actor: "u_admin", action: services.OrgRemoveMember, target: services.OrgTarget{UserID: "u_owner"}, wantErr: "only an owner can change or remove an owner"},
		{name: "unknown target", members: members, actor: "u_owner", action: services.OrgRemoveMember, target: services.OrgTarget{UserID: "u_nobody"}, wantErr: "member not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authz, _ := newAuthorizer(t, tt.members)
			policy, err := services.NewOrgPolicy(services.OrgPolicyDeps{Memberships: tt.members, Authz: authz, Logger: slog.New(slog.DiscardHandler)})
			require.NoError(t, err)

			err = policy.Check(context.Background(), services.Actor{UserID: tt.actor, WorkspaceID: "ws_1"}, tt.action, tt.target)

			if tt.wantErr != "" {
				var appErr *apperr.AppError
				require.ErrorAs(t, err, &appErr)
				require.Equal(t, tt.wantErr, appErr.Message)
				return
			}
			require.NoError(t, err)
		})
	}
	t.Run("a developer cannot manage members at all", func(t *testing.T) {
		authz, roles := newAuthorizer(t, members)
		roles.EXPECT().GetMemberRole(gomock.Any(), "ws_1", "u_dev").Return("", apperr.ErrNotFound)
		policy, err := services.NewOrgPolicy(services.OrgPolicyDeps{Memberships: members, Authz: authz, Logger: slog.New(slog.DiscardHandler)})
		require.NoError(t, err)
		require.ErrorIs(t, policy.Check(context.Background(), services.Actor{UserID: "u_dev", WorkspaceID: "ws_1"}, services.OrgInvite, services.OrgTarget{}), apperr.ErrForbidden)
	})
}
