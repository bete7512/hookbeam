package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// CreateApplication registers a consumer application in the active workspace.
func (h *Handler) CreateApplication(w http.ResponseWriter, r *http.Request) {
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
	app, err := h.deps.Delivery.CreateApplication(r.Context(), a, req.Name)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toApplicationDTO(app))
}

// UpdateApplication changes an application's settings.
func (h *Handler) UpdateApplication(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.UpdateApplicationRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	app, err := h.deps.Delivery.UpdateApplication(r.Context(), a, applicationID, services.UpdateApplicationInput{ResponsePreviewsEnabled: req.ResponsePreviewsEnabled})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

func toApplicationDTO(a *models.Application) openapi.Application {
	return openapi.Application{Id: a.ID, WorkspaceId: a.WorkspaceID, EnvironmentId: a.EnvironmentID, EnvironmentKind: openapi.EnvironmentKind(a.EnvironmentKind), Name: a.Name, CreatedBy: a.CreatedBy, ResponsePreviewsEnabled: a.ResponsePreviewsEnabled, CreatedAt: a.CreatedAt, ArchivedAt: a.ArchivedAt}
}

// ArchiveApplication retires an application with its endpoints and their unsent deliveries.
func (h *Handler) ArchiveApplication(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	app, endpoints, cancelled, err := h.deps.Delivery.ArchiveApplication(r.Context(), a, applicationID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.ApplicationArchived{Application: toApplicationDTO(app), ArchivedEndpoints: endpoints, CancelledDeliveries: cancelled})
}

// GetApplication reads one application.
func (h *Handler) GetApplication(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	app, err := h.deps.Delivery.GetApplication(r.Context(), a, applicationID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toApplicationDTO(app))
}

// ListEnvironmentApplications lists an environment's applications.
func (h *Handler) ListEnvironmentApplications(w http.ResponseWriter, r *http.Request, environmentID openapi.EnvironmentId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	apps, err := h.deps.Delivery.ListApplications(r.Context(), a, environmentID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.ApplicationList{Items: make([]openapi.Application, 0, len(apps))}
	for i := range apps {
		resp.Items = append(resp.Items, toApplicationDTO(&apps[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}
