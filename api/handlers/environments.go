package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// CreateEnvironment adds a development or production environment to the active workspace.
func (h *Handler) CreateEnvironment(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.CreateEnvironmentRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	e, err := h.deps.Delivery.CreateEnvironment(r.Context(), a, req.Name, string(req.Kind))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toEnvironmentDTO(e))
}

// ListEnvironments returns the active workspace's environments.
func (h *Handler) ListEnvironments(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.deps.Delivery.ListEnvironments(r.Context(), a)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.EnvironmentList{Items: make([]openapi.Environment, 0, len(list))}
	for i := range list {
		resp.Items = append(resp.Items, toEnvironmentDTO(&list[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// CreateEnvironmentApplication registers a consumer application in the environment.
func (h *Handler) CreateEnvironmentApplication(w http.ResponseWriter, r *http.Request, environmentID openapi.EnvironmentId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.CreateApplicationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	app, err := h.deps.Delivery.CreateApplicationIn(r.Context(), a, environmentID, req.Name)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toApplicationDTO(app))
}

func toEnvironmentDTO(e *models.Environment) openapi.Environment {
	return openapi.Environment{Id: e.ID, WorkspaceId: e.WorkspaceID, Name: e.Name, Kind: openapi.EnvironmentKind(e.Kind), CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt}
}
