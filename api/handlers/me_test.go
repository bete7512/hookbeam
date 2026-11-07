package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
)

func TestMe(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"me", http.MethodGet, "/v1/me", "", nil, http.StatusOK, "", true},
	})

	t.Run("user session body", func(t *testing.T) {
		f := newFixture(t)

		rec := f.do(http.MethodGet, "/v1/me", "")

		require.Equal(t, http.StatusOK, rec.Code)
		var me openapi.Me
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
		require.NotNil(t, me.UserId)
		assert.Equal(t, ada.UserID, *me.UserId)
		require.NotNil(t, me.WorkspaceId)
		assert.Equal(t, workspace, *me.WorkspaceId)
		assert.Equal(t, ada.Methods, me.Methods)
		require.NotNil(t, me.ExpiresAt)
		assert.True(t, ada.ExpiresAt.Equal(*me.ExpiresAt))
	})

	t.Run("session without a workspace", func(t *testing.T) {
		f := newFixture(t)
		f.auth.principal.OrganizationID = ""

		rec := f.do(http.MethodGet, "/v1/me", "")

		require.Equal(t, http.StatusOK, rec.Code)
		var me openapi.Me
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
		assert.Nil(t, me.WorkspaceId)
	})

	t.Run("api key body", func(t *testing.T) {
		f := newFixture(t)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_good").Return(types.Principal{OrganizationID: "ws_key", Methods: []string{"api_key"}}, &models.APIKey{ID: 9}, nil)
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", "Bearer hb_good")

		rec := f.send(req)

		require.Equal(t, http.StatusOK, rec.Code)
		var me openapi.Me
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
		assert.Nil(t, me.UserId)
		assert.Nil(t, me.ExpiresAt, "a key without expiry has no expiresAt")
		require.NotNil(t, me.WorkspaceId)
		assert.Equal(t, "ws_key", *me.WorkspaceId)
		assert.Equal(t, []string{"api_key"}, me.Methods)
	})
}
