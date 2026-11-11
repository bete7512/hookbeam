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

// CreateApiKey issues a workspace-wide key and returns the secret once.
func (h *Handler) CreateApiKey(w http.ResponseWriter, r *http.Request) {
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
	key := toAPIKeyModel(&req, nil)
	raw, err := h.deps.APIKeys.CreateAPIKey(r.Context(), a, key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toAPIKeyCreatedDTO(key, raw))
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

// CreateApplicationApiKey issues a key that may submit only to the application and returns the secret once.
func (h *Handler) CreateApplicationApiKey(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
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
	key := toAPIKeyModel(&req, &applicationID)
	raw, err := h.deps.APIKeys.CreateAPIKey(r.Context(), a, key)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toAPIKeyCreatedDTO(key, raw))
}

func toAPIKeyModel(req *openapi.CreateApiKeyRequest, applicationID *int64) *models.APIKey {
	key := &models.APIKey{
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		ApplicationID: applicationID,
	}
	if req.Scopes != nil {
		for _, s := range *req.Scopes {
			key.Scopes = append(key.Scopes, string(s))
		}
	}
	return key
}

// toAPIKeyCreatedDTO is the create response: the key's record plus the raw secret, shown this once.
func toAPIKeyCreatedDTO(k *models.APIKey, raw string) openapi.ApiKeyCreated {
	dto := openapi.ApiKeyCreated{
		Id:            k.ID,
		WorkspaceId:   k.WorkspaceID,
		Name:          k.Name,
		Prefix:        k.Prefix,
		ApplicationId: k.ApplicationID,
		CreatedBy:     k.CreatedBy,
		ExpiresAt:     k.ExpiresAt,
		CreatedAt:     k.CreatedAt,
		UpdatedAt:     k.UpdatedAt,
		Key:           raw,
		Scopes:        make([]openapi.ApiKeyCreatedScopes, 0, len(k.Scopes)),
	}
	for _, s := range k.Scopes {
		dto.Scopes = append(dto.Scopes, openapi.ApiKeyCreatedScopes(s))
	}
	return dto
}

func toAPIKeyDTO(k *models.APIKeyForList) openapi.ApiKey {
	dto := openapi.ApiKey{
		Id:            k.ID,
		WorkspaceId:   k.WorkspaceID,
		Name:          k.Name,
		Prefix:        k.Prefix,
		CreatedBy:     k.CreatedBy,
		ApplicationId: k.ApplicationID,
		ExpiresAt:     k.ExpiresAt,
		LastUsedAt:    k.LastUsedAt,
		RevokedAt:     k.RevokedAt,
		CreatedAt:     k.CreatedAt,
		UpdatedAt:     k.UpdatedAt,
		Scopes:        make([]openapi.ApiKeyScopes, 0, len(k.Scopes)),
	}
	for _, s := range k.Scopes {
		dto.Scopes = append(dto.Scopes, openapi.ApiKeyScopes(s))
	}
	return dto
}
