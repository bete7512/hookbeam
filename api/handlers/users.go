package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/auth"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

const defaultPageSize = 20

// CreateUser registers a new user.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req openapi.CreateUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	user := &models.User{Email: req.Email, Name: req.Name}
	if err := h.deps.Users.Register(ctx, user, req.Password); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toUserDTO(user))
}

// GetUser returns a user by id.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request, id openapi.UserId) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	user, err := h.deps.Users.GetUserFor(r.Context(), claims.UserID, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toUserDTO(user))
}

// ListUsers returns a page restricted to the authenticated user.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request, params openapi.ListUsersParams) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	opts := repos.ListUsersOpts{Limit: defaultPageSize}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}
	if params.Offset != nil {
		opts.Offset = *params.Offset
	}
	if params.Search != nil {
		opts.Search = *params.Search
	}
	if params.SortBy != nil {
		opts.SortBy = string(*params.SortBy)
	}
	if params.SortDir != nil {
		opts.SortDir = string(*params.SortDir)
	}

	users, total, err := h.deps.Users.ListUsers(r.Context(), claims.UserID, opts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	resp := openapi.UserList{Items: make([]openapi.User, 0, len(users)), Total: total}
	for _, u := range users {
		resp.Items = append(resp.Items, toUserListDTO(u))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// UpdateUser changes a user's profile.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request, id openapi.UserId) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	var req openapi.UpdateUserRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	user := &models.User{ID: id, Name: req.Name}
	if err := h.deps.Users.UpdateUser(r.Context(), claims.UserID, user); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toUserDTO(user))
}

// DeleteUser deletes a user.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request, id openapi.UserId) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	if err := h.deps.Users.DeleteUser(r.Context(), claims.UserID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ChangeUserPassword replaces a user's password after checking the current one.
func (h *Handler) ChangeUserPassword(w http.ResponseWriter, r *http.Request, id openapi.UserId) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	var req openapi.ChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	if err := h.deps.Users.ChangePassword(r.Context(), claims.UserID, id, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetMe returns the authenticated user.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}
	user, err := h.deps.Users.GetUserFor(r.Context(), claims.UserID, claims.UserID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toUserDTO(user))
}

func toUserDTO(u *models.User) openapi.User {
	return openapi.User{
		Id:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		EmailVerified: u.EmailVerifiedAt != nil,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}

func toUserListDTO(u models.UserForList) openapi.User {
	return openapi.User{
		Id:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		EmailVerified: u.EmailVerifiedAt != nil,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}
