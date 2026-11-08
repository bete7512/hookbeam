package services_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

type APIKeyServiceSuite struct {
	suite.Suite
	keys    *repo_mocks.MockAPIKeyRepo
	audit   *repo_mocks.MockAuditRepo
	audited []models.AuditEvent
	members *fakeMemberships
	roles   *repo_mocks.MockMemberRoleRepo
	svc     services.APIKeyService
}

func TestAPIKeyServiceSuite(t *testing.T) {
	suite.Run(t, new(APIKeyServiceSuite))
}

const (
	owner  = "u_owner"
	admin  = "u_admin"
	member = "u_member"
	ws     = "ws_1"
)

func (s *APIKeyServiceSuite) SetupSubTest() {
	ctrl := gomock.NewController(s.T())
	s.keys = repo_mocks.NewMockAPIKeyRepo(ctrl)
	s.keys.EXPECT().WithTx(gomock.Any()).Return(s.keys).AnyTimes()
	s.audit = repo_mocks.NewMockAuditRepo(ctrl)
	s.audit.EXPECT().WithTx(gomock.Any()).Return(s.audit).AnyTimes()
	s.audited = nil
	s.audit.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		s.audited = append(s.audited, *e)
		return nil
	}).AnyTimes()
	s.members = &fakeMemberships{roles: map[string]string{owner: "owner", admin: "admin", member: "member"}}
	s.roles = repo_mocks.NewMockMemberRoleRepo(ctrl)
	s.roles.EXPECT().GetMemberRole(gomock.Any(), gomock.Any(), gomock.Any()).Return("", apperr.ErrNotFound).AnyTimes()
	authz, err := services.NewAuthorizer(services.AuthorizerDeps{Memberships: s.members, Roles: s.roles, Logger: slog.New(slog.DiscardHandler)})
	s.Require().NoError(err)
	s.svc, err = services.NewAPIKeyService(services.APIKeyServiceDeps{Tx: fakeTx{}, Keys: s.keys, Audit: s.audit, Authz: authz, Logger: slog.New(slog.DiscardHandler)})
	s.Require().NoError(err)
}

func (s *APIKeyServiceSuite) requireErr(err error, wantStatus int, wantFields []string) {
	s.T().Helper()
	if wantStatus == 0 {
		s.Require().NoError(err)
		return
	}
	var appErr *apperr.AppError
	s.Require().ErrorAs(err, &appErr, "%v", err)
	s.Equal(wantStatus, appErr.Code)
	for _, f := range wantFields {
		s.Contains(appErr.Fields, f)
	}
}

func (s *APIKeyServiceSuite) TestNew() {
	_, err := services.NewAPIKeyService(services.APIKeyServiceDeps{Keys: s.keys})
	s.Error(err)
}

func (s *APIKeyServiceSuite) TestCreateAPIKey() {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	app7, zero := int64(7), int64(0)
	tests := []struct {
		name       string
		actor      services.Actor
		key        models.APIKey
		setup      func()
		wantStatus int
		wantFields []string
	}{
		{name: "owner creates with defaults", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: "  prod  "}, setup: func() {
			s.keys.EXPECT().CreateAPIKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, k *models.APIKey) error {
				s.Equal("prod", k.Name)
				s.Equal(ws, k.WorkspaceID)
				s.Equal(owner, k.CreatedBy)
				s.Equal([]string{models.ScopeMessagesWrite}, k.Scopes)
				s.True(strings.HasPrefix(k.Prefix, models.APIKeyPrefix))
				s.Len(k.KeyHash, 64)
				k.ID = 1
				return nil
			})
		}},
		{name: "admin with expiry", actor: services.Actor{UserID: admin, WorkspaceID: ws}, key: models.APIKey{Name: "prod", ExpiresAt: &future}, setup: func() {
			s.keys.EXPECT().CreateAPIKey(gomock.Any(), gomock.Any()).Return(nil)
		}},
		{name: "member is forbidden", actor: services.Actor{UserID: member, WorkspaceID: ws}, key: models.APIKey{Name: "prod"}, wantStatus: 403},
		{name: "non-member is forbidden", actor: services.Actor{UserID: "u_stranger", WorkspaceID: ws}, key: models.APIKey{Name: "prod"}, wantStatus: 403},
		{name: "no workspace", actor: services.Actor{UserID: owner}, key: models.APIKey{Name: "prod"}, wantStatus: 403},
		{name: "no user", actor: services.Actor{WorkspaceID: ws}, key: models.APIKey{Name: "prod"}, wantStatus: 403},
		{name: "validation", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: " ", Scopes: []string{"admin"}, ExpiresAt: &past}, wantStatus: 422, wantFields: []string{"name", "scopes", "expiresAt"}},
		{name: "bound to an application with every scope", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: "sender", Scopes: models.APIKeyScopes, ApplicationID: &app7}, setup: func() {
			s.keys.EXPECT().CreateAPIKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, k *models.APIKey) error {
				s.Equal(int64(7), *k.ApplicationID)
				s.Len(k.Scopes, 4)
				return nil
			})
		}},
		{name: "bad application id", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: "sender", ApplicationID: &zero}, wantStatus: 422, wantFields: []string{"applicationId"}},
		{name: "name too long", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: strings.Repeat("x", 101)}, wantStatus: 422, wantFields: []string{"name"}},
		{name: "membership lookup fails", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: "prod"}, setup: func() { s.members.err = errors.New("boom") }, wantStatus: 500},
		{name: "repo failure", actor: services.Actor{UserID: owner, WorkspaceID: ws}, key: models.APIKey{Name: "prod"}, setup: func() {
			s.keys.EXPECT().CreateAPIKey(gomock.Any(), gomock.Any()).Return(errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}
			key := tt.key

			raw, err := s.svc.CreateAPIKey(context.Background(), tt.actor, &key)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus != 0 {
				return
			}
			s.True(strings.HasPrefix(raw, models.APIKeyPrefix))
			s.Greater(len(raw), 40)
			s.Equal(raw[:len(key.Prefix)], key.Prefix)
			s.NotContains(key.KeyHash, raw[len(models.APIKeyPrefix):])
			s.Require().Len(s.audited, 1, "one audit row in the transaction")
			s.Equal(services.AuditKeyCreate, s.audited[0].Action)
			s.NotContains(string(s.audited[0].Details), raw[len(models.APIKeyPrefix):], "the secret never lands in the audit trail")
		})
	}
}

func (s *APIKeyServiceSuite) TestListAPIKeys() {
	actor := services.Actor{UserID: member, WorkspaceID: ws}
	tests := []struct {
		name       string
		actor      services.Actor
		opts       repos.ListAPIKeysOpts
		setup      func()
		wantTotal  int
		wantStatus int
		wantFields []string
	}{
		{name: "member lists", actor: actor, opts: repos.ListAPIKeysOpts{Limit: 20, Search: " prod "}, setup: func() {
			want := repos.ListAPIKeysOpts{WorkspaceID: ws, Limit: 20, Search: "prod"}
			s.keys.EXPECT().ListAPIKeys(gomock.Any(), want).Return([]models.APIKeyForList{{ID: 1}}, nil)
			s.keys.EXPECT().CountAPIKeys(gomock.Any(), want).Return(1, nil)
		}, wantTotal: 1},
		{name: "limit out of range", actor: actor, opts: repos.ListAPIKeysOpts{Limit: 101}, wantStatus: 422, wantFields: []string{"limit"}},
		{name: "bad sort", actor: actor, opts: repos.ListAPIKeysOpts{Limit: 20, SortBy: "secret", SortDir: "sideways"}, wantStatus: 422, wantFields: []string{"sort_by", "sort_dir"}},
		{name: "no workspace", actor: services.Actor{UserID: member}, opts: repos.ListAPIKeysOpts{Limit: 20}, wantStatus: 403},
		{name: "repo failure", actor: actor, opts: repos.ListAPIKeysOpts{Limit: 20}, setup: func() {
			s.keys.EXPECT().ListAPIKeys(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			_, total, err := s.svc.ListAPIKeys(context.Background(), tt.actor, tt.opts)

			s.requireErr(err, tt.wantStatus, tt.wantFields)
			if tt.wantStatus == 0 {
				s.Equal(tt.wantTotal, total)
			}
		})
	}
}

func (s *APIKeyServiceSuite) TestRevokeAPIKey() {
	tests := []struct {
		name       string
		actor      services.Actor
		setup      func()
		wantStatus int
	}{
		{name: "admin revokes and it is audited", actor: services.Actor{UserID: admin, WorkspaceID: ws}, setup: func() {
			s.keys.EXPECT().RevokeAPIKey(gomock.Any(), ws, int64(7)).Return(nil)
		}},
		{name: "member is forbidden", actor: services.Actor{UserID: member, WorkspaceID: ws}, wantStatus: 403},
		{name: "unknown or foreign key", actor: services.Actor{UserID: owner, WorkspaceID: ws}, setup: func() {
			s.keys.EXPECT().RevokeAPIKey(gomock.Any(), ws, int64(7)).Return(apperr.ErrNotFound)
		}, wantStatus: 404},
		{name: "repo failure", actor: services.Actor{UserID: owner, WorkspaceID: ws}, setup: func() {
			s.keys.EXPECT().RevokeAPIKey(gomock.Any(), ws, int64(7)).Return(errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			err := s.svc.RevokeAPIKey(context.Background(), tt.actor, 7)

			s.requireErr(err, tt.wantStatus, nil)
			if tt.wantStatus == 0 {
				s.Require().Len(s.audited, 1)
				s.Equal(services.AuditKeyRevoke, s.audited[0].Action)
				s.Equal(int64(7), *s.audited[0].ResourceID)
			}
		})
	}
}

func (s *APIKeyServiceSuite) TestAuthenticate() {
	expires := time.Now().Add(time.Hour)
	tests := []struct {
		name       string
		raw        string
		setup      func()
		want       types.Principal
		wantStatus int
	}{
		{name: "valid key", raw: "hb_secret", setup: func() {
			s.keys.EXPECT().FindActiveAPIKeyByHash(gomock.Any(), gomock.Len(64)).Return(&models.APIKey{ID: 7, WorkspaceID: ws, ExpiresAt: &expires, Scopes: []string{models.ScopeMessagesWrite}}, nil)
			s.keys.EXPECT().TouchAPIKey(gomock.Any(), int64(7)).Return(errors.New("touch is best effort"))
		}, want: types.Principal{OrganizationID: ws, Methods: []string{"api_key"}, ExpiresAt: expires}},
		{name: "wrong prefix never reaches the repo", raw: "eyJhbGciOi", wantStatus: 401},
		{name: "unknown, revoked or expired", raw: "hb_nope", setup: func() {
			s.keys.EXPECT().FindActiveAPIKeyByHash(gomock.Any(), gomock.Any()).Return(nil, apperr.ErrNotFound)
		}, wantStatus: 401},
		{name: "repo failure", raw: "hb_nope", setup: func() {
			s.keys.EXPECT().FindActiveAPIKeyByHash(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
		}, wantStatus: 500},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.setup != nil {
				tt.setup()
			}

			p, key, err := s.svc.Authenticate(context.Background(), tt.raw)

			s.requireErr(err, tt.wantStatus, nil)
			if tt.wantStatus != 0 {
				return
			}
			s.Equal([]string{models.ScopeMessagesWrite}, key.Scopes)
			s.Equal(int64(7), key.ID)
			s.Empty(key.KeyHash, "the hash never leaves the service")
			s.Equal(tt.want.OrganizationID, p.OrganizationID)
			s.Equal(tt.want.Methods, p.Methods)
			s.True(tt.want.ExpiresAt.Equal(p.ExpiresAt))
			s.Empty(p.UserID)
			s.False(p.AuthenticatedAt.IsZero())
		})
	}
}
