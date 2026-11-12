package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestWorkspaceLimits(t *testing.T) {
	limits := &models.WorkspaceLimits{EndpointsPerApplication: 20, FanoutPerMessage: 20, BacklogPerWorkspace: 10000, Backlog: 12, Applications: []models.ApplicationEndpointCount{{ApplicationID: 7, Name: "Customer A", Endpoints: 3}}, Retention: models.Retention{Payload: 168 * time.Hour, IdempotencyWindow: 168 * time.Hour, Audit: 2160 * time.Hour}}
	runEndpoints(t, []endpointCase{
		{"limits", http.MethodGet, "/v1/workspaces/current/limits", "", func(f *fixture) {
			f.delivery.EXPECT().GetWorkspaceLimits(gomock.Any(), adaActor).Return(limits, nil)
		}, http.StatusOK, "", true},
		{"data plane down answers 503 with Retry-After", http.MethodGet, "/v1/workspaces/current/limits", "", func(f *fixture) {
			e := apperr.NewUnavailable("delivery service unavailable", nil)
			e.RetryAfter = 5 * 1e9
			f.delivery.EXPECT().GetWorkspaceLimits(gomock.Any(), adaActor).Return(nil, e)
		}, http.StatusServiceUnavailable, apperr.CodeUnavailable, true},
	})
	t.Run("an api key is refused before the handler", func(t *testing.T) {
		f := newFixture(t)
		req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/current/limits", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "does not accept api keys")
	})
	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().GetWorkspaceLimits(gomock.Any(), adaActor).Return(limits, nil)
		rec := f.do(http.MethodGet, "/v1/workspaces/current/limits", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.WorkspaceLimits
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, 10000, got.Limits.BacklogPerWorkspace)
		assert.Equal(t, 12, got.Usage.Backlog)
		require.Len(t, got.Usage.Applications, 1)
		assert.Equal(t, int32(3), got.Usage.Applications[0].Endpoints)
	})
	t.Run("retry-after header on 503", func(t *testing.T) {
		f := newFixture(t)
		e := apperr.NewUnavailable("delivery service unavailable", nil)
		e.RetryAfter = 5 * 1e9
		f.delivery.EXPECT().GetWorkspaceLimits(gomock.Any(), adaActor).Return(nil, e)
		rec := f.do(http.MethodGet, "/v1/workspaces/current/limits", "")
		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Equal(t, "5", rec.Header().Get("Retry-After"))
	})
}
