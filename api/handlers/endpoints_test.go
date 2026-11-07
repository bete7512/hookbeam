package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestEndpoints(t *testing.T) {
	endpoint := &models.Endpoint{ID: 3, ApplicationID: 7, URL: "https://receiver.example.com/hooks", Revision: 1, EventTypes: []string{"order.shipped"}, CreatedAt: ada.AuthenticatedAt}
	in := services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL, Description: "prod", EventTypes: []string{"order.shipped"}}
	paused := *endpoint
	now := time.Now()
	paused.PausedAt = &now
	runEndpoints(t, []endpointCase{
		{"create", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","description":"prod","eventTypes":["order.shipped"]}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, in).Return(endpoint, "whsec_abc", int32(1), nil)
		}, http.StatusCreated, "", true},
		{"create for an unknown application", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL}).Return(nil, "", int32(0), apperr.NewNotFound("application", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"create with a bad application id", http.MethodPost, "/v1/applications/zero/endpoints", `{"url":"https://x"}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"create with a bad body", http.MethodPost, "/v1/applications/7/endpoints", `{"url":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"pause", http.MethodPost, "/v1/endpoints/3/pause", "", func(f *fixture) {
			f.delivery.EXPECT().PauseEndpoint(gomock.Any(), adaActor, int64(3)).Return(&paused, nil)
		}, http.StatusOK, "", true},
		{"pause an archived endpoint", http.MethodPost, "/v1/endpoints/3/pause", "", func(f *fixture) {
			f.delivery.EXPECT().PauseEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"pause as an api key is refused", http.MethodPost, "/v1/endpoints/3/pause", "", func(f *fixture) {
			f.auth.principal = ada
			f.auth.principal.UserID = ""
		}, http.StatusForbidden, apperr.CodeForbidden, false},
		{"update url and subscriptions", http.MethodPatch, "/v1/endpoints/3", `{"url":"https://new.example.com/hooks","eventTypes":["order.paid"]}`, func(f *fixture) {
			url, types := "https://new.example.com/hooks", []string{"order.paid"}
			updated := *endpoint
			updated.URL, updated.Revision, updated.EventTypes = url, 2, types
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{EndpointID: 3, URL: &url, EventTypes: &types}).Return(&updated, nil)
		}, http.StatusOK, "", true},
		{"update with an empty body is a validation error", http.MethodPatch, "/v1/endpoints/3", `{}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{EndpointID: 3}).Return(nil, apperr.NewValidation(map[string]string{"body": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"update an archived endpoint", http.MethodPatch, "/v1/endpoints/3", `{"description":"x"}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, gomock.Any()).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"update with a bad body", http.MethodPatch, "/v1/endpoints/3", `{"url":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"resume", http.MethodPost, "/v1/endpoints/3/resume", "", func(f *fixture) {
			f.delivery.EXPECT().ResumeEndpoint(gomock.Any(), adaActor, int64(3)).Return(endpoint, nil)
		}, http.StatusOK, "", true},
		{"resume an archived endpoint", http.MethodPost, "/v1/endpoints/3/resume", "", func(f *fixture) {
			f.delivery.EXPECT().ResumeEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"archive", http.MethodDelete, "/v1/endpoints/3", "", func(f *fixture) {
			archived := *endpoint
			archived.ArchivedAt = &now
			f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(&archived, int64(2), nil)
		}, http.StatusOK, "", true},
		{"archive an unknown endpoint", http.MethodDelete, "/v1/endpoints/3", "", func(f *fixture) {
			f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, int64(0), apperr.NewNotFound("endpoint", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
	})
	t.Run("archive body", func(t *testing.T) {
		f := newFixture(t)
		archived := *endpoint
		archived.ArchivedAt = &now
		f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(&archived, int64(2), nil)
		rec := f.do(http.MethodDelete, "/v1/endpoints/3", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.EndpointArchived
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, int64(2), got.CancelledDeliveries)
		assert.NotNil(t, got.Endpoint.ArchivedAt)
	})

	t.Run("create body carries the secret once", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL}).Return(endpoint, "whsec_abc", int32(1), nil)
		rec := f.do(http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks"}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created openapi.EndpointCreated
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "whsec_abc", created.SigningSecret)
		assert.Equal(t, int32(1), created.SecretVersion)
		assert.Equal(t, int64(3), created.Endpoint.Id)
		assert.Equal(t, []string{"order.shipped"}, created.Endpoint.EventTypes)
		assert.Nil(t, created.Endpoint.PausedAt)
	})
}
