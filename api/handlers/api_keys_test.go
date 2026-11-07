package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

var (
	adaActor = services.Actor{UserID: ada.UserID, WorkspaceID: workspace}
	prodKey  = models.APIKeyForList{ID: 7, WorkspaceID: workspace, Name: "production", Prefix: "hb_9Xk2pQ4r", Scopes: []string{models.ScopeMessagesWrite}, CreatedBy: ada.UserID, CreatedAt: ada.AuthenticatedAt, UpdatedAt: ada.AuthenticatedAt}
)

func TestAPIKeys(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"list", http.MethodGet, "/v1/api-keys?limit=10&sort_by=name&sort_dir=desc", "", func(f *fixture) {
			f.apiKeys.EXPECT().ListAPIKeys(gomock.Any(), adaActor, repos.ListAPIKeysOpts{Limit: 10, SortBy: "name", SortDir: "desc"}).Return([]models.APIKeyForList{prodKey}, 1, nil)
		}, http.StatusOK, "", true},
		{"list defaults", http.MethodGet, "/v1/api-keys", "", func(f *fixture) {
			f.apiKeys.EXPECT().ListAPIKeys(gomock.Any(), adaActor, repos.ListAPIKeysOpts{Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"list bad limit binds before the service", http.MethodGet, "/v1/api-keys?limit=abc", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"list invalid sort reaches the service whitelist", http.MethodGet, "/v1/api-keys?sort_by=secret", "", func(f *fixture) {
			f.apiKeys.EXPECT().ListAPIKeys(gomock.Any(), adaActor, repos.ListAPIKeysOpts{Limit: 20, SortBy: "secret"}).Return(nil, 0, apperr.NewValidation(map[string]string{"sort_by": "unknown sort key"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"list without a workspace", http.MethodGet, "/v1/api-keys", "", func(f *fixture) { f.auth.principal.OrganizationID = "" }, http.StatusForbidden, apperr.CodeForbidden, true},
		{"create", http.MethodPost, "/v1/api-keys", `{"name":"production","scopes":["messages:write"]}`, func(f *fixture) {
			f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).DoAndReturn(func(_ any, _ services.Actor, k *models.APIKey) (string, error) {
				assert.Equal(t, "production", k.Name)
				assert.Equal(t, []string{models.ScopeMessagesWrite}, k.Scopes)
				k.ID, k.WorkspaceID, k.Prefix, k.CreatedBy, k.CreatedAt, k.UpdatedAt = 7, workspace, "hb_9Xk2pQ4r", ada.UserID, ada.AuthenticatedAt, ada.AuthenticatedAt
				return "hb_9Xk2pQ4rSECRET", nil
			})
		}, http.StatusCreated, "", true},
		{"create malformed", http.MethodPost, "/v1/api-keys", `{"name":`, nil, http.StatusBadRequest, apperr.CodeBadRequest, true},
		{"create unknown scope reaches the service whitelist", http.MethodPost, "/v1/api-keys", `{"name":"x","scopes":["admin"]}`, func(f *fixture) {
			f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).DoAndReturn(func(_ any, _ services.Actor, k *models.APIKey) (string, error) {
				assert.Equal(t, []string{"admin"}, k.Scopes)
				return "", apperr.NewValidation(map[string]string{"scopes": "unknown scope admin"})
			})
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"create validation", http.MethodPost, "/v1/api-keys", `{"name":""}`, func(f *fixture) {
			f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).Return("", apperr.NewValidation(map[string]string{"name": "is required"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"create as member", http.MethodPost, "/v1/api-keys", `{"name":"x"}`, func(f *fixture) {
			f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).Return("", apperr.NewForbidden("workspace admin required", nil))
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"create service failure", http.MethodPost, "/v1/api-keys", `{"name":"x"}`, func(f *fixture) {
			f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).Return("", apperr.NewInternal(errors.New("boom")))
		}, http.StatusInternalServerError, apperr.CodeInternal, true},
		{"revoke", http.MethodDelete, "/v1/api-keys/7", "", func(f *fixture) {
			f.apiKeys.EXPECT().RevokeAPIKey(gomock.Any(), adaActor, int64(7)).Return(nil)
		}, http.StatusNoContent, "", true},
		{"revoke unknown", http.MethodDelete, "/v1/api-keys/999", "", func(f *fixture) {
			f.apiKeys.EXPECT().RevokeAPIKey(gomock.Any(), adaActor, int64(999)).Return(apperr.NewNotFound("api key", apperr.ErrNotFound))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"revoke bad id", http.MethodDelete, "/v1/api-keys/nope", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})

	t.Run("create body carries the secret once", func(t *testing.T) {
		f := newFixture(t)
		expires := ada.ExpiresAt.Add(24 * time.Hour)
		f.apiKeys.EXPECT().CreateAPIKey(gomock.Any(), adaActor, gomock.Any()).DoAndReturn(func(_ any, _ services.Actor, k *models.APIKey) (string, error) {
			require.NotNil(t, k.ExpiresAt)
			k.ID, k.WorkspaceID, k.Prefix, k.CreatedBy, k.Scopes = 7, workspace, "hb_9Xk2pQ4r", ada.UserID, []string{models.ScopeMessagesWrite}
			return "hb_9Xk2pQ4rSECRET", nil
		})

		rec := f.do(http.MethodPost, "/v1/api-keys", `{"name":"production","expiresAt":"`+expires.Format(time.RFC3339)+`"}`)

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created openapi.ApiKeyCreated
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "hb_9Xk2pQ4rSECRET", created.Key)
		assert.Equal(t, int64(7), created.Id)
		assert.Equal(t, workspace, created.WorkspaceId)
		assert.Equal(t, []openapi.ApiKeyCreatedScopes{openapi.ApiKeyCreatedScopesMessagesWrite}, created.Scopes)
	})

	t.Run("list body", func(t *testing.T) {
		f := newFixture(t)
		f.apiKeys.EXPECT().ListAPIKeys(gomock.Any(), adaActor, gomock.Any()).Return([]models.APIKeyForList{prodKey}, 1, nil)

		rec := f.do(http.MethodGet, "/v1/api-keys", "")

		require.Equal(t, http.StatusOK, rec.Code)
		var list openapi.ApiKeyList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		require.Len(t, list.Items, 1)
		assert.Equal(t, 1, list.Total)
		assert.Equal(t, "production", list.Items[0].Name)
		assert.Equal(t, "hb_9Xk2pQ4r", list.Items[0].Prefix)
		assert.NotContains(t, rec.Body.String(), "key_hash")
	})
}
