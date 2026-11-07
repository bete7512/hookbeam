package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// SubmitMessage accepts an event for the application; the raw body is the outbound payload.
func (h *Handler) SubmitMessage(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId, params openapi.SubmitMessageParams) {
	p, err := principal(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	body, err := httpx.ReadBody(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	res, err := h.deps.Delivery.SubmitMessage(r.Context(), p.OrganizationID, services.SubmitMessageInput{ApplicationID: applicationID, IdempotencyKey: params.IdempotencyKey, Payload: body})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusAccepted, openapi.MessageAccepted{
		MessageId:     res.Message.ID,
		WebhookId:     res.Message.WebhookID,
		Status:        openapi.Accepted,
		DeliveryCount: res.Message.DeliveryCount,
		AcceptedAt:    res.Message.AcceptedAt,
		Duplicate:     res.Duplicate,
	})
}

// GetMessage returns a message with its deliveries.
func (h *Handler) GetMessage(w http.ResponseWriter, r *http.Request, messageID openapi.MessageId) {
	p, err := principal(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if p.OrganizationID == "" {
		httpx.WriteError(w, r, apperr.NewForbidden("select a workspace", nil))
		return
	}
	m, deliveries, err := h.deps.Delivery.GetMessage(r.Context(), p.OrganizationID, messageID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.MessageDetail{Message: toMessageDTO(m), Deliveries: make([]openapi.Delivery, 0, len(deliveries))}
	for i := range deliveries {
		resp.Deliveries = append(resp.Deliveries, toDeliveryDTO(&deliveries[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

func toMessageDTO(m *models.Message) openapi.Message {
	return openapi.Message{Id: m.ID, WebhookId: m.WebhookID, ApplicationId: m.ApplicationID, Type: m.Type, ExternalId: m.ExternalID, AcceptedAt: m.AcceptedAt, DeliveryCount: m.DeliveryCount}
}

func toDeliveryDTO(d *models.Delivery) openapi.Delivery {
	return openapi.Delivery{
		Id: d.ID, MessageId: d.MessageID, EndpointId: d.EndpointID, State: openapi.DeliveryState(d.State), AttemptCount: d.AttemptCount,
		DueAt: d.DueAt, DeadlineAt: d.DeadlineAt, FailureReason: d.FailureReason, ReplayOfDeliveryId: d.ReplayOfDeliveryID, CreatedAt: d.CreatedAt, CompletedAt: d.CompletedAt,
	}
}
