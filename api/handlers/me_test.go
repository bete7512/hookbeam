package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/gen/openapi"
)

func TestMe(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"me", http.MethodGet, "/v1/me", "", nil, http.StatusOK, "", true},
	})

	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.auth.principal.OrganizationID = "ws_1"

		rec := f.do(http.MethodGet, "/v1/me", "")

		require.Equal(t, http.StatusOK, rec.Code)
		var me openapi.Me
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &me))
		assert.Equal(t, ada.UserID, me.UserId)
		require.NotNil(t, me.WorkspaceId)
		assert.Equal(t, "ws_1", *me.WorkspaceId)
		assert.Equal(t, ada.Methods, me.Methods)
		assert.True(t, ada.ExpiresAt.Equal(me.ExpiresAt))
	})
}
