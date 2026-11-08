package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// ListDeliveryAttempts returns every attempt of a delivery, oldest first.
func (h *Handler) ListDeliveryAttempts(w http.ResponseWriter, r *http.Request, deliveryID openapi.DeliveryId) {
	a, err := actorOrKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in := services.ListAttemptsInput{DeliveryID: deliveryID}
	if key, ok := apiKeyFrom(r); ok {
		in.KeyScopes = key.Scopes
	}
	attempts, err := h.deps.Delivery.ListAttempts(r.Context(), a, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.AttemptList{Items: make([]openapi.Attempt, 0, len(attempts))}
	for i := range attempts {
		resp.Items = append(resp.Items, toAttemptDTO(&attempts[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// CancelDelivery stops further attempts of a delivery; a finished delivery is returned unchanged.
func (h *Handler) CancelDelivery(w http.ResponseWriter, r *http.Request, deliveryID openapi.DeliveryId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := h.deps.Delivery.CancelDelivery(r.Context(), a, deliveryID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toDeliveryDTO(d))
}

// ReplayDelivery starts a new delivery cycle for a failed or cancelled delivery.
func (h *Handler) ReplayDelivery(w http.ResponseWriter, r *http.Request, deliveryID openapi.DeliveryId, params openapi.ReplayDeliveryParams) {
	a, err := actorOrKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := h.deps.Delivery.ReplayDelivery(r.Context(), a, deliveryID, params.IdempotencyKey)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusCreated, toDeliveryDTO(d))
}

func toAttemptDTO(a *models.Attempt) openapi.Attempt {
	out := openapi.Attempt{
		Id: a.ID, DeliveryId: a.DeliveryID, Number: a.Number, StartedAt: a.StartedAt, CompletedAt: a.CompletedAt, StatusCode: a.StatusCode,
		ErrorClass: a.ErrorClass, LatencyMs: a.LatencyMS, SecretVersion: a.SecretVersion, EndpointRevision: a.EndpointRevision, RequestUrl: a.RequestURL,
		ResponsePreview: a.ResponsePreview,
	}
	if a.ResponseHeaders != nil {
		headers := a.ResponseHeaders
		out.ResponseHeaders = &headers
	}
	return out
}
