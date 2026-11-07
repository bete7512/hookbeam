package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/apps/api/gen/openapi"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/pkg/apperr"
)

func TestUsers(t *testing.T) {
	id := strconv.FormatInt(ada.ID, 10)
	createBody := `{"email":"ada@example.com","name":"Ada","password":"correct horse battery"}`

	runEndpoints(t, []endpointCase{
		{"create", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), "correct horse battery").
				DoAndReturn(func(_ context.Context, u *models.User, _ string) error { *u = ada; return nil })
		}, http.StatusCreated, "", false},
		{"create validation", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(apperr.NewValidation(map[string]string{"password": "too short"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, false},
		{"create conflict", http.MethodPost, "/v1/users", createBody, func(f *fixture) {
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(apperr.NewConflict("email already registered", nil))
		}, http.StatusConflict, apperr.CodeConflict, false},
		{"create malformed json", http.MethodPost, "/v1/users", `{"email":`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"create unknown field", http.MethodPost, "/v1/users", `{"extra":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"get", http.MethodGet, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, ada.ID).Return(&ada, nil)
		}, http.StatusOK, "", true},
		{"get bad id", http.MethodGet, "/v1/users/nope", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false}, // binding fails before auth
		{"get not found", http.MethodGet, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, ada.ID).Return(nil, apperr.NewNotFound("user", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"list", http.MethodGet, "/v1/users", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), ada.ID, repos.ListUsersOpts{Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"list with params", http.MethodGet, "/v1/users?limit=5&offset=10&search=ada", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), ada.ID, repos.ListUsersOpts{Limit: 5, Offset: 10, Search: "ada"}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"list sorted", http.MethodGet, "/v1/users?sort_by=email&sort_dir=desc", "", func(f *fixture) {
			f.users.EXPECT().ListUsers(gomock.Any(), ada.ID, repos.ListUsersOpts{Limit: 20, SortBy: "email", SortDir: "desc"}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"list bad limit", http.MethodGet, "/v1/users?limit=abc", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false}, // binding fails before auth
		{"update", http.MethodPatch, "/v1/users/" + id, `{"name":"Ada King"}`, func(f *fixture) {
			f.users.EXPECT().UpdateUser(gomock.Any(), ada.ID, gomock.Any()).Return(nil)
		}, http.StatusOK, "", true},
		{"update not found", http.MethodPatch, "/v1/users/" + id, `{"name":"Ada King"}`, func(f *fixture) {
			f.users.EXPECT().UpdateUser(gomock.Any(), ada.ID, gomock.Any()).Return(apperr.NewNotFound("user", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"delete", http.MethodDelete, "/v1/users/" + id, "", func(f *fixture) {
			f.users.EXPECT().DeleteUser(gomock.Any(), ada.ID, ada.ID).Return(nil)
		}, http.StatusNoContent, "", true},
		{"change password", http.MethodPut, "/v1/users/" + id + "/password", `{"currentPassword":"old password 12","newPassword":"new password 12"}`, func(f *fixture) {
			f.users.EXPECT().ChangePassword(gomock.Any(), ada.ID, ada.ID, "old password 12", "new password 12").Return(nil)
		}, http.StatusNoContent, "", true},
		{"change password wrong current", http.MethodPut, "/v1/users/" + id + "/password", `{"currentPassword":"x","newPassword":"new password 12"}`, func(f *fixture) {
			f.users.EXPECT().ChangePassword(gomock.Any(), ada.ID, ada.ID, gomock.Any(), gomock.Any()).
				Return(apperr.NewValidation(map[string]string{"currentPassword": "is incorrect"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"me", http.MethodGet, "/v1/me", "", func(f *fixture) {
			f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, ada.ID).Return(&ada, nil)
		}, http.StatusOK, "", true},
	})
}

func TestListUsersResponse(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().ListUsers(gomock.Any(), ada.ID, repos.ListUsersOpts{Limit: 20}).
		Return([]models.UserForList{{ID: ada.ID, Email: ada.Email, Name: ada.Name}}, 7, nil)

	rec := f.do(http.MethodGet, "/v1/users", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var page openapi.UserList
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	assert.Equal(t, 7, page.Total)
	require.Len(t, page.Items, 1)
	assert.Equal(t, ada.Email, page.Items[0].Email)
}

func TestUserOwnershipResponses(t *testing.T) {
	other := ada.ID + 1
	path := "/v1/users/" + strconv.FormatInt(other, 10)
	denied := apperr.NewForbidden("access to another user is not permitted", nil)
	runEndpoints(t, []endpointCase{
		{"read another user", http.MethodGet, path, "", func(f *fixture) {
			f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, other).Return(nil, denied)
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"update another user", http.MethodPatch, path, `{"name":"Changed"}`, func(f *fixture) {
			f.users.EXPECT().UpdateUser(gomock.Any(), ada.ID, gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, user *models.User) error {
				assert.Equal(t, other, user.ID)
				return denied
			})
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"delete another user", http.MethodDelete, path, "", func(f *fixture) {
			f.users.EXPECT().DeleteUser(gomock.Any(), ada.ID, other).Return(denied)
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"change another user's password", http.MethodPut, path + "/password", `{"currentPassword":"current-password","newPassword":"new-password"}`, func(f *fixture) {
			f.users.EXPECT().ChangePassword(gomock.Any(), ada.ID, other, gomock.Any(), gomock.Any()).Return(denied)
		}, http.StatusForbidden, apperr.CodeForbidden, true},
	})
}

func TestInternalErrorIsLoggedNotLeaked(t *testing.T) {
	f := newFixture(t)
	f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, ada.ID).Return(nil, apperr.NewInternal(errors.New("pg: connection refused")))

	rec := f.do(http.MethodGet, "/v1/users/"+strconv.FormatInt(ada.ID, 10), "")

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.NotContains(t, rec.Body.String(), "connection refused")

	var line map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(f.logs.Bytes()), &line))
	assert.Equal(t, "ERROR", line["level"])
	assert.Equal(t, "GET /v1/users/{id}", line["route"])
	assert.Contains(t, line["error"], "connection refused")
}
