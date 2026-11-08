//go:build security

package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// TestSecurityRoleMatrix pins who may do what: viewers never reach payloads or replay, developers never manage
// keys or members, admins never touch owners.
func TestSecurityRoleMatrix(t *testing.T) {
	members := &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "member", "u_viewer": "member"}}
	authz, roles := newAuthorizer(t, members)
	roles.EXPECT().GetMemberRole(gomock.Any(), "ws_1", "u_viewer").Return("viewer", nil).AnyTimes()
	roles.EXPECT().GetMemberRole(gomock.Any(), "ws_1", "u_dev").Return("", apperr.ErrNotFound).AnyTimes()
	denied := []struct {
		user string
		cap  services.Capability
	}{
		{"u_viewer", services.CapReadPayloads}, {"u_viewer", services.CapReplay}, {"u_viewer", services.CapManageEndpoints}, {"u_viewer", services.CapManageKeys}, {"u_viewer", services.CapManageMembers},
		{"u_dev", services.CapManageEndpoints}, {"u_dev", services.CapManageKeys}, {"u_dev", services.CapManageMembers}, {"u_dev", services.CapManageOwners},
		{"u_admin", services.CapManageOwners},
	}
	for _, d := range denied {
		err := authz.Can(context.Background(), services.Actor{UserID: d.user, WorkspaceID: "ws_1"}, d.cap)
		require.ErrorIs(t, err, apperr.ErrForbidden, "%s %s", d.user, d.cap)
	}
	for _, c := range []services.Capability{services.CapReadMetadata, services.CapReadPayloads, services.CapReplay, services.CapManageEndpoints, services.CapManageKeys, services.CapManageMembers, services.CapManageOwners} {
		require.NoError(t, authz.Can(context.Background(), services.Actor{UserID: "u_owner", WorkspaceID: "ws_1"}, c))
	}
	require.NoError(t, authz.Can(context.Background(), services.Actor{UserID: "u_viewer", WorkspaceID: "ws_1"}, services.CapReadMetadata))
	require.ErrorIs(t, authz.Can(context.Background(), services.Actor{UserID: "u_stranger", WorkspaceID: "ws_1"}, services.CapReadMetadata), apperr.ErrForbidden, "a non-member reads nothing")
}

// TestSecurityPrivilegeEscalation: nobody grants above their own role, only owners touch owners, and the
// workspace keeps at least one owner, through invitations, role changes and removals alike.
func TestSecurityPrivilegeEscalation(t *testing.T) {
	members := &fakeMemberships{roles: map[string]string{"u_owner": "owner", "u_admin": "admin", "u_dev": "member"}}
	authz, roles := newAuthorizer(t, members)
	roles.EXPECT().GetMemberRole(gomock.Any(), gomock.Any(), gomock.Any()).Return("", apperr.ErrNotFound).AnyTimes()
	policy, err := services.NewOrgPolicy(services.OrgPolicyDeps{Memberships: members, Authz: authz, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	ctx := context.Background()
	refused := []struct {
		name   string
		actor  string
		action services.OrgAction
		target services.OrgTarget
	}{
		{"admin invites an owner", "u_admin", services.OrgInvite, services.OrgTarget{Email: "x@example.com", Role: "owner"}},
		{"admin promotes to owner", "u_admin", services.OrgChangeRole, services.OrgTarget{UserID: "u_dev", Role: "owner"}},
		{"admin demotes an owner", "u_admin", services.OrgChangeRole, services.OrgTarget{UserID: "u_owner", Role: "member"}},
		{"admin removes an owner", "u_admin", services.OrgRemoveMember, services.OrgTarget{UserID: "u_owner"}},
		{"developer invites anyone", "u_dev", services.OrgInvite, services.OrgTarget{Email: "x@example.com", Role: "member"}},
		{"the last owner steps down", "u_owner", services.OrgChangeRole, services.OrgTarget{UserID: "u_owner", Role: "admin"}},
		{"the last owner is removed", "u_owner", services.OrgRemoveMember, services.OrgTarget{UserID: "u_owner"}},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Check(ctx, services.Actor{UserID: tt.actor, WorkspaceID: "ws_1"}, tt.action, tt.target)
			require.Error(t, err)
			require.True(t, errors.Is(err, apperr.ErrForbidden) || errors.Is(err, apperr.ErrConflict) || errors.Is(err, apperr.ErrValidation), "%v", err)
		})
	}
	require.NoError(t, policy.Check(ctx, services.Actor{UserID: "u_admin", WorkspaceID: "ws_1"}, services.OrgInvite, services.OrgTarget{Email: "x@example.com", Role: "member"}))
	require.NoError(t, policy.Check(ctx, services.Actor{UserID: "u_owner", WorkspaceID: "ws_1"}, services.OrgChangeRole, services.OrgTarget{UserID: "u_dev", Role: "owner"}))
}
