package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestMembers(t *testing.T) {
	members := []models.Member{{UserID: "u_ada", Role: "owner", JoinedAt: ada.AuthenticatedAt}, {UserID: "u_bob", Role: "viewer", JoinedAt: ada.AuthenticatedAt}}
	runEndpoints(t, []endpointCase{
		{"list", http.MethodGet, "/v1/members", "", func(f *fixture) {
			f.members.EXPECT().List(gomock.Any(), adaActor).Return(members, nil)
		}, http.StatusOK, "", true},
		{"set role", http.MethodPut, "/v1/members/u_bob/role", `{"role":"viewer"}`, func(f *fixture) {
			f.members.EXPECT().SetRole(gomock.Any(), adaActor, "u_bob", "viewer").Return(&members[1], nil)
		}, http.StatusOK, "", true},
		{"set role as a viewer is forbidden", http.MethodPut, "/v1/members/u_bob/role", `{"role":"viewer"}`, func(f *fixture) {
			f.members.EXPECT().SetRole(gomock.Any(), adaActor, "u_bob", "viewer").Return(nil, apperr.NewForbidden("a viewer cannot manage members and invitations", nil))
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"set role on an admin", http.MethodPut, "/v1/members/u_bob/role", `{"role":"viewer"}`, func(f *fixture) {
			f.members.EXPECT().SetRole(gomock.Any(), adaActor, "u_bob", "viewer").Return(nil, apperr.NewConflict("an admin keeps that role", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"set an unknown role", http.MethodPut, "/v1/members/u_bob/role", `{"role":"god"}`, func(f *fixture) {
			f.members.EXPECT().SetRole(gomock.Any(), adaActor, "u_bob", "god").Return(nil, apperr.NewValidation(map[string]string{"role": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"set role with a bad body", http.MethodPut, "/v1/members/u_bob/role", `{"role":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})
	t.Run("list body", func(t *testing.T) {
		f := newFixture(t)
		f.members.EXPECT().List(gomock.Any(), adaActor).Return(members, nil)
		rec := f.do(http.MethodGet, "/v1/members", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.MemberList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 2)
		assert.Equal(t, openapi.MemberRole("viewer"), got.Items[1].Role)
	})
}

// TestOrgRouteGuard puts the membership policy in front of goauth's invite, role-change and remove routes.
func TestOrgRouteGuard(t *testing.T) {
	served := map[string]int{}
	route := func(name, method, path string) goauthcfg.RouteInfo {
		return goauthcfg.RouteInfo{Name: name, Method: method, Path: authBasePath + path, Handler: func(w http.ResponseWriter, _ *http.Request) {
			served[name]++
			w.WriteHeader(http.StatusAccepted)
		}}
	}
	routes := []goauthcfg.RouteInfo{
		route("org.invite", http.MethodPost, "/org/{orgId}/invite"),
		route("org.members.update", http.MethodPut, "/org/{orgId}/members/{userId}"),
		route("org.members.remove", http.MethodDelete, "/org/{orgId}/members/{userId}"),
		route("org.members.list", http.MethodGet, "/org/{orgId}/members"),
	}
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		token      string
		setup      func(f *fixture)
		wantStatus int
	}{
		{name: "invite passes the policy, reaches goauth and is audited", method: http.MethodPost, path: "/org/ws_1/invite", body: `{"email":"x@example.com","role":"admin"}`, token: "good", setup: func(f *fixture) {
			f.policy.EXPECT().Check(gomock.Any(), services.Actor{UserID: "u_ada", WorkspaceID: "ws_1"}, services.OrgInvite, services.OrgTarget{Email: "x@example.com", Role: "admin"}).Return(nil)
			f.audit.EXPECT().Record(gomock.Any(), services.Actor{UserID: "u_ada", WorkspaceID: "ws_1"}, services.AuditOrgInvite, "member", int64(0), map[string]any{"email": "x@example.com", "role": "admin"}).Return(nil)
		}, wantStatus: http.StatusAccepted},
		{name: "invite above the inviter's role is refused before goauth", method: http.MethodPost, path: "/org/ws_1/invite", body: `{"email":"x@example.com","role":"owner"}`, token: "good", setup: func(f *fixture) {
			f.policy.EXPECT().Check(gomock.Any(), gomock.Any(), services.OrgInvite, gomock.Any()).Return(apperr.NewForbidden("an admin cannot invite an owner", nil))
		}, wantStatus: http.StatusForbidden},
		{name: "role change carries the target user and role", method: http.MethodPut, path: "/org/ws_1/members/u_bob", body: `{"role":"owner"}`, token: "good", setup: func(f *fixture) {
			f.policy.EXPECT().Check(gomock.Any(), gomock.Any(), services.OrgChangeRole, services.OrgTarget{UserID: "u_bob", Role: "owner"}).Return(nil)
			f.audit.EXPECT().Record(gomock.Any(), gomock.Any(), services.AuditOrgChangeRole, "member", int64(0), gomock.Any()).Return(nil)
		}, wantStatus: http.StatusAccepted},
		{name: "removing the last owner is refused", method: http.MethodDelete, path: "/org/ws_1/members/u_ada", token: "good", setup: func(f *fixture) {
			f.policy.EXPECT().Check(gomock.Any(), gomock.Any(), services.OrgRemoveMember, services.OrgTarget{UserID: "u_ada"}).Return(apperr.NewForbidden("the workspace must keep at least one owner", nil))
		}, wantStatus: http.StatusForbidden},
		{name: "unguarded goauth route passes through untouched", method: http.MethodGet, path: "/org/ws_1/members", token: "", wantStatus: http.StatusAccepted},
		{name: "no session", method: http.MethodPost, path: "/org/ws_1/invite", body: `{}`, token: "", wantStatus: http.StatusUnauthorized},
		{name: "api key is refused", method: http.MethodPost, path: "/org/ws_1/invite", body: `{}`, token: "hb_key", wantStatus: http.StatusUnauthorized},
		{name: "invalid body", method: http.MethodPost, path: "/org/ws_1/invite", body: `{`, token: "good", wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixtureWithRouter(t, handlers.RouterOptions{AuthRoutes: routes})
			if tt.setup != nil {
				tt.setup(f)
			}
			req := httptest.NewRequest(tt.method, authBasePath+tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := f.send(req)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}
