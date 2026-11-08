package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestEnvironments(t *testing.T) {
	prod := &models.Environment{ID: 2, WorkspaceID: workspace, Name: "production", Kind: "production", CreatedBy: ada.UserID, CreatedAt: ada.AuthenticatedAt}
	app := &models.Application{ID: 1, WorkspaceID: workspace, EnvironmentID: 2, EnvironmentKind: "production", Name: "Customer A", CreatedBy: ada.UserID, CreatedAt: ada.AuthenticatedAt}
	runEndpoints(t, []endpointCase{
		{"create", http.MethodPost, "/v1/environments", `{"name":"production","kind":"production"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEnvironment(gomock.Any(), adaActor, "production", "production").Return(prod, nil)
		}, http.StatusCreated, "", true},
		{"create with a bad kind is refused by the data plane", http.MethodPost, "/v1/environments", `{"name":"x","kind":"staging"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEnvironment(gomock.Any(), adaActor, "x", "staging").Return(nil, apperr.NewValidation(map[string]string{"kind": "must be development or production"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"create conflict", http.MethodPost, "/v1/environments", `{"name":"production","kind":"production"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEnvironment(gomock.Any(), adaActor, "production", "production").Return(nil, apperr.NewConflict("exists", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"list", http.MethodGet, "/v1/environments", "", func(f *fixture) {
			f.delivery.EXPECT().ListEnvironments(gomock.Any(), adaActor).Return([]models.Environment{*prod}, nil)
		}, http.StatusOK, "", true},
		{"create an application in an environment", http.MethodPost, "/v1/environments/2/applications", `{"name":"Customer A"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplicationIn(gomock.Any(), adaActor, int64(2), "Customer A").Return(app, nil)
		}, http.StatusCreated, "", true},
		{"create an application in an unknown environment", http.MethodPost, "/v1/environments/9/applications", `{"name":"Customer A"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplicationIn(gomock.Any(), adaActor, int64(9), "Customer A").Return(nil, apperr.NewNotFound("environment", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
	})
	t.Run("application body carries the environment", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().CreateApplicationIn(gomock.Any(), adaActor, int64(2), "Customer A").Return(app, nil)
		rec := f.do(http.MethodPost, "/v1/environments/2/applications", `{"name":"Customer A"}`)
		require.Equal(t, http.StatusCreated, rec.Code)
		var got openapi.Application
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, int64(2), got.EnvironmentId)
		assert.Equal(t, openapi.EnvironmentKind("production"), got.EnvironmentKind)
	})
}
