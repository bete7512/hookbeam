package handlers_test

import (
	"errors"
	"net/http"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
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
	})
}
