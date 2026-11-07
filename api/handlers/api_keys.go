package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

const defaultPageSize = 20

// ListApiKeys lists the active workspace's keys.
func (h *Handler) ListApiKeys(w http.ResponseWriter, r *http.Request, params openapi.ListApiKeysParams) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	opts := repos.ListAPIKeysOpts{Limit: defaultPageSize}
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

	keys, total, err := h.deps.APIKeys.ListAPIKeys(r.Context(), a, opts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.ApiKeyList{Items: make([]openapi.ApiKey, 0, len(keys)), Total: total}
	for i := range keys {
		resp.Items = append(resp.Items, toAPIKeyDTO(&keys[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// CreateApiKey issues a key for the active workspace and returns the secret once.
func (h *Handler) CreateApiKey(w http.ResponseWriter, r *http.Request) {
	h.createKey(w, r, nil)
}

// createKey mints a key, bound to applicationID when given.
func (h *Handler) createKey(w http.ResponseWriter, r *http.Request, applicationID *int64) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.CreateApiKeyRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key := &models.APIKey{Name: req.Name, ExpiresAt: req.ExpiresAt, ApplicationID: applicationID}
	if req.Scopes != nil {
		for _, s := range *req.Scopes {
			key.Scopes = append(key.Scopes, string(s))
		}
	}
	raw, err := h.deps.APIKeys.CreateAPIKey(r.Context(), a, key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	dto := toAPIKeyDTO(&models.APIKeyForList{ID: key.ID, WorkspaceID: key.WorkspaceID, Name: key.Name, Prefix: key.Prefix, Scopes: key.Scopes, ApplicationID: key.ApplicationID, CreatedBy: key.CreatedBy, ExpiresAt: key.ExpiresAt, CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt})
	created := openapi.ApiKeyCreated{Id: dto.Id, WorkspaceId: dto.WorkspaceId, Name: dto.Name, Prefix: dto.Prefix, ApplicationId: dto.ApplicationId, CreatedBy: dto.CreatedBy, ExpiresAt: dto.ExpiresAt, CreatedAt: dto.CreatedAt, UpdatedAt: dto.UpdatedAt, Key: raw, Scopes: make([]openapi.ApiKeyCreatedScopes, 0, len(dto.Scopes))}
	for _, s := range dto.Scopes {
		created.Scopes = append(created.Scopes, openapi.ApiKeyCreatedScopes(s))
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, created)
}

// RevokeApiKey revokes one of the active workspace's keys.
func (h *Handler) RevokeApiKey(w http.ResponseWriter, r *http.Request, id openapi.ApiKeyId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.deps.APIKeys.RevokeAPIKey(r.Context(), a, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreateApplicationApiKey mints a key that may submit only to the application.
func (h *Handler) CreateApplicationApiKey(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	h.createKey(w, r, &applicationID)
}

func toAPIKeyDTO(k *models.APIKeyForList) openapi.ApiKey {
	dto := openapi.ApiKey{
		Id: k.ID, WorkspaceId: k.WorkspaceID, Name: k.Name, Prefix: k.Prefix, CreatedBy: k.CreatedBy, ApplicationId: k.ApplicationID,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
		Scopes: make([]openapi.ApiKeyScopes, 0, len(k.Scopes)),
	}
	for _, s := range k.Scopes {
		dto.Scopes = append(dto.Scopes, openapi.ApiKeyScopes(s))
	}
	return dto
}
