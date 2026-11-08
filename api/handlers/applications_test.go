package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestApplications(t *testing.T) {
	app := &models.Application{ID: 1, WorkspaceID: workspace, Name: "Customer A", CreatedBy: ada.UserID, CreatedAt: ada.AuthenticatedAt}
	runEndpoints(t, []endpointCase{
		{"create", http.MethodPost, "/v1/applications", `{"name":"Customer A"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplication(gomock.Any(), adaActor, "Customer A").Return(app, nil)
		}, http.StatusCreated, "", true},
		{"create conflict", http.MethodPost, "/v1/applications", `{"name":"Customer A"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplication(gomock.Any(), adaActor, "Customer A").Return(nil, apperr.NewConflict("exists", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"create validation from the data plane", http.MethodPost, "/v1/applications", `{"name":" "}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplication(gomock.Any(), adaActor, " ").Return(nil, apperr.NewValidation(map[string]string{"name": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"create with a bad body", http.MethodPost, "/v1/applications", `{"name":`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"create without a workspace", http.MethodPost, "/v1/applications", `{"name":"x"}`, func(f *fixture) {
			f.auth.principal.OrganizationID = ""
		}, http.StatusForbidden, apperr.CodeForbidden, false},
		{"create when the data plane is down", http.MethodPost, "/v1/applications", `{"name":"x"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateApplication(gomock.Any(), adaActor, "x").Return(nil, apperr.NewUnavailable("delivery service unavailable", errors.New("down")))
		}, http.StatusServiceUnavailable, apperr.CodeUnavailable, true},
		{"enable response previews", http.MethodPatch, "/v1/applications/1", `{"responsePreviewsEnabled":true}`, func(f *fixture) {
			on := true
			f.delivery.EXPECT().UpdateApplication(gomock.Any(), adaActor, int64(1), services.UpdateApplicationInput{ResponsePreviewsEnabled: &on}).Return(&models.Application{ID: 1, ResponsePreviewsEnabled: true}, nil)
		}, http.StatusOK, "", true},
		{"update refused for a developer", http.MethodPatch, "/v1/applications/1", `{"responsePreviewsEnabled":true}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateApplication(gomock.Any(), adaActor, int64(1), gomock.Any()).Return(nil, apperr.NewForbidden("a developer cannot manage endpoints", nil))
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"update with nothing to change", http.MethodPatch, "/v1/applications/1", `{}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateApplication(gomock.Any(), adaActor, int64(1), services.UpdateApplicationInput{}).Return(nil, apperr.NewValidation(map[string]string{"responsePreviewsEnabled": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"update with an unknown field", http.MethodPatch, "/v1/applications/1", `{"name":"x"}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"get", http.MethodGet, "/v1/applications/1", "", func(f *fixture) {
			f.delivery.EXPECT().GetApplication(gomock.Any(), adaActor, int64(1)).Return(app, nil)
		}, http.StatusOK, "", true},
		{"get unknown", http.MethodGet, "/v1/applications/1", "", func(f *fixture) {
			f.delivery.EXPECT().GetApplication(gomock.Any(), adaActor, int64(1)).Return(nil, apperr.NewNotFound("application", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"list an environment's applications", http.MethodGet, "/v1/environments/2/applications", "", func(f *fixture) {
			f.delivery.EXPECT().ListApplications(gomock.Any(), adaActor, int64(2)).Return([]models.Application{*app}, nil)
		}, http.StatusOK, "", true},
		{"list for an unknown environment", http.MethodGet, "/v1/environments/9/applications", "", func(f *fixture) {
			f.delivery.EXPECT().ListApplications(gomock.Any(), adaActor, int64(9)).Return(nil, apperr.NewNotFound("environment", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
	})
	t.Run("list body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().ListApplications(gomock.Any(), adaActor, int64(2)).Return([]models.Application{*app}, nil)
		rec := f.do(http.MethodGet, "/v1/environments/2/applications", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.ApplicationList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 1)
		require.Equal(t, "Customer A", got.Items[0].Name)
	})
	t.Run("the body carries the setting", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().UpdateApplication(gomock.Any(), adaActor, int64(1), gomock.Any()).Return(&models.Application{ID: 1, ResponsePreviewsEnabled: true, CreatedAt: ada.AuthenticatedAt}, nil)
		rec := f.do(http.MethodPatch, "/v1/applications/1", `{"responsePreviewsEnabled":true}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.Application
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.True(t, got.ResponsePreviewsEnabled)
	})
}
