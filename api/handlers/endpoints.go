package handlers

import (
	"net/http"
	"time"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// defaultSecretOverlap is how long a rotated-out secret keeps signing unless the caller says otherwise.
const defaultSecretOverlap = 24 * time.Hour

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
	_ = httpx.WriteJSON(w, http.StatusCreated, openapi.EndpointCreated{
		Endpoint:      toEndpointDTO(endpoint),
		SigningSecret: secret,
		SecretVersion: version,
	})
}

// GetEndpoint reads an endpoint with its subscriptions and health.
func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endpoint, err := h.deps.Delivery.GetEndpoint(r.Context(), a, endpointID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toEndpointDTO(endpoint))
}

// ListApplicationEndpoints lists an application's endpoints with their health.
func (h *Handler) ListApplicationEndpoints(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	list, err := h.deps.Delivery.ListEndpoints(r.Context(), a, applicationID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.EndpointList{Items: make([]openapi.Endpoint, 0, len(list))}
	for i := range list {
		resp.Items = append(resp.Items, toEndpointDTO(&list[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// SendTestMessage plans a test delivery for the endpoint.
func (h *Handler) SendTestMessage(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, d, err := h.deps.Delivery.SendTestMessage(r.Context(), a, endpointID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusAccepted, openapi.TestMessageSent{
		Message:  toMessageDTO(m),
		Delivery: toDeliveryDTO(d),
	})
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

// UpdateEndpoint changes the URL, description or subscriptions and bumps the revision.
func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.UpdateEndpointRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	endpoint, err := h.deps.Delivery.UpdateEndpoint(r.Context(), a, services.UpdateEndpointInput{
		EndpointID:       endpointID,
		URL:              req.Url,
		Description:      req.Description,
		EventTypes:       req.EventTypes,
		ExpectedRevision: req.ExpectedRevision,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toEndpointDTO(endpoint))
}

// RotateEndpointSecret adds a signing secret and retires the previous ones after the overlap.
func (h *Handler) RotateEndpointSecret(w http.ResponseWriter, r *http.Request, endpointID openapi.EndpointId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	retireIn := defaultSecretOverlap
	if r.ContentLength != 0 {
		var req openapi.RotateSecretRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if req.RetireIn != nil {
			if retireIn, err = time.ParseDuration(*req.RetireIn); err != nil {
				httpx.WriteError(w, r, apperr.NewValidation(map[string]string{"retireIn": "must be a duration such as 24h or 0s"}))
				return
			}
		}
	}
	rotated, err := h.deps.Delivery.RotateEndpointSecret(r.Context(), a, endpointID, retireIn)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.SecretRotated{
		Endpoint:      toEndpointDTO(rotated.Endpoint),
		SigningSecret: rotated.SigningSecret,
		SecretVersion: rotated.SecretVersion,
		RetireAt:      rotated.RetireAt,
	})
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
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.EndpointArchived{
		Endpoint:            toEndpointDTO(endpoint),
		CancelledDeliveries: cancelled,
	})
}

func toEndpointDTO(e *models.Endpoint) openapi.Endpoint {
	out := openapi.Endpoint{
		Id:            e.ID,
		ApplicationId: e.ApplicationID,
		Url:           e.URL,
		Description:   e.Description,
		Revision:      e.Revision,
		EventTypes:    e.EventTypes,
		PausedAt:      e.PausedAt,
		ArchivedAt:    e.ArchivedAt,
		CreatedAt:     e.CreatedAt,
		Waiting:       e.Waiting,
		Health: openapi.EndpointHealth{
			State:               openapi.EndpointHealthState(e.Health.State),
			ObservedAt:          e.Health.ObservedAt,
			Stale:               e.Health.Stale,
			ConsecutiveFailures: e.Health.ConsecutiveFailures,
		},
	}
	if e.Secrets != nil {
		secrets := make([]openapi.EndpointSecretInfo, 0, len(e.Secrets))
		for _, s := range e.Secrets {
			secrets = append(secrets, openapi.EndpointSecretInfo{
				Version:     s.Version,
				Fingerprint: s.Fingerprint,
				CreatedAt:   s.CreatedAt,
				RetireAt:    s.RetireAt,
			})
		}
		out.Secrets = &secrets
	}
	return out
}
