package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestNotifications(t *testing.T) {
	notice := models.Notification{ID: 5, WorkspaceID: workspace, UserID: ada.UserID, Kind: "failure", EpisodeID: 9, EndpointID: 3, EndpointURL: "https://r", ApplicationID: 7, Title: "Endpoint unhealthy: https://r", Body: "b", CreatedAt: ada.AuthenticatedAt}
	read := notice
	read.ReadAt = &ada.ExpiresAt
	runEndpoints(t, []endpointCase{
		{"list unread", http.MethodGet, "/v1/notifications?unread=true&limit=5", "", func(f *fixture) {
			f.notes.EXPECT().List(gomock.Any(), adaActor, repos.ListNotificationsOpts{UnreadOnly: true, Limit: 5}).Return([]models.Notification{notice}, 1, 1, nil)
		}, http.StatusOK, "", true},
		{"list defaults", http.MethodGet, "/v1/notifications", "", func(f *fixture) {
			f.notes.EXPECT().List(gomock.Any(), adaActor, repos.ListNotificationsOpts{Limit: 20}).Return(nil, 0, 0, nil)
		}, http.StatusOK, "", true},
		{"list bad limit binds before the service", http.MethodGet, "/v1/notifications?limit=abc", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"mark read", http.MethodPost, "/v1/notifications/5/read", "", func(f *fixture) {
			f.notes.EXPECT().MarkRead(gomock.Any(), adaActor, int64(5)).Return(&read, nil)
		}, http.StatusOK, "", true},
		{"mark read unknown", http.MethodPost, "/v1/notifications/5/read", "", func(f *fixture) {
			f.notes.EXPECT().MarkRead(gomock.Any(), adaActor, int64(5)).Return(nil, apperr.NewNotFound("notification", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"mark read bad id", http.MethodPost, "/v1/notifications/x/read", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"preferences", http.MethodGet, "/v1/me/notification-preferences", "", func(f *fixture) {
			f.notes.EXPECT().Preferences(gomock.Any(), adaActor).Return(&models.NotificationPreferences{UserID: ada.UserID, EmailAlerts: true}, nil)
		}, http.StatusOK, "", true},
		{"set preferences", http.MethodPut, "/v1/me/notification-preferences", `{"emailAlerts":false}`, func(f *fixture) {
			f.notes.EXPECT().SetPreferences(gomock.Any(), adaActor, false).Return(&models.NotificationPreferences{UserID: ada.UserID, EmailAlerts: false}, nil)
		}, http.StatusOK, "", true},
		{"set preferences with a bad body", http.MethodPut, "/v1/me/notification-preferences", `{"emailAlerts":"yes"}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})
	t.Run("an api key is refused before the handler", func(t *testing.T) {
		f := newFixture(t)
		req := httptest.NewRequest(http.MethodGet, "/v1/notifications", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "does not accept api keys")
	})
	t.Run("list body", func(t *testing.T) {
		f := newFixture(t)
		f.notes.EXPECT().List(gomock.Any(), adaActor, gomock.Any()).Return([]models.Notification{read}, 1, 3, nil)
		rec := f.do(http.MethodGet, "/v1/notifications", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.NotificationList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Len(t, got.Items, 1)
		assert.Equal(t, 1, got.Total)
		assert.Equal(t, 3, got.Unread)
		assert.Equal(t, openapi.NotificationKind("failure"), got.Items[0].Kind)
		assert.Equal(t, int64(9), got.Items[0].EpisodeId)
		assert.Equal(t, "https://r", got.Items[0].EndpointUrl)
		assert.NotNil(t, got.Items[0].ReadAt)
	})
	t.Run("preferences body", func(t *testing.T) {
		f := newFixture(t)
		f.notes.EXPECT().SetPreferences(gomock.Any(), services.Actor{UserID: ada.UserID, WorkspaceID: workspace}, false).Return(&models.NotificationPreferences{EmailAlerts: false}, nil)
		rec := f.do(http.MethodPut, "/v1/me/notification-preferences", `{"emailAlerts":false}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.NotificationPreferences
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.False(t, got.EmailAlerts)
	})
}
