package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// CreateEndpoint registers a destination and returns its signing secret once.
func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.CreateEndpointRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in := services.CreateEndpointInput{ApplicationID: applicationID, URL: req.Url}
	if req.Description != nil {
		in.Description = *req.Description
	}
	if req.EventTypes != nil {
		in.EventTypes = *req.EventTypes
	}
	endpoint, secret, version, err := h.deps.Delivery.CreateEndpoint(r.Context(), a, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, openapi.EndpointCreated{Endpoint: toEndpointDTO(endpoint), SigningSecret: secret, SecretVersion: version})
}

// PauseEndpoint stops new attempts for the endpoint.
func (h *Handler) PauseEndpoint(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endpoint, err := h.deps.Delivery.PauseEndpoint(r.Context(), a, endpointID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toEndpointDTO(endpoint))
}

// ResumeEndpoint lets a paused endpoint's backlog drain again.
func (h *Handler) ResumeEndpoint(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endpoint, err := h.deps.Delivery.ResumeEndpoint(r.Context(), a, endpointID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toEndpointDTO(endpoint))
}

// ArchiveEndpoint retires an endpoint and cancels its unsent deliveries.
func (h *Handler) ArchiveEndpoint(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endpoint, cancelled, err := h.deps.Delivery.ArchiveEndpoint(r.Context(), a, endpointID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.EndpointArchived{Endpoint: toEndpointDTO(endpoint), CancelledDeliveries: cancelled})
}

func toEndpointDTO(e *models.Endpoint) openapi.Endpoint {
	return openapi.Endpoint{
		Id: e.ID, ApplicationId: e.ApplicationID, Url: e.URL, Description: e.Description, Revision: e.Revision, EventTypes: e.EventTypes,
		PausedAt: e.PausedAt, ArchivedAt: e.ArchivedAt, CreatedAt: e.CreatedAt,
	}
}
