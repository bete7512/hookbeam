package handlers

import (
	"net/http"
	"strconv"
	"time"

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
	if key, ok := apiKeyFrom(r); ok {
		if key.ApplicationID != nil && *key.ApplicationID != applicationID {
			httpx.WriteError(w, r, apperr.NewForbidden("api key is bound to another application", nil))
			return
		}
		decision, err := h.deps.Limiter.Allow(r.Context(), "api-key:"+strconv.FormatInt(key.ID, 10), h.deps.KeySubmitLimit, time.Minute)
		if err != nil {
			httpx.WriteError(w, r, apperr.NewInternal(err))
			return
		}
		if !decision.Allowed {
			httpx.WriteError(w, r, apperr.NewTooManyRequests(decision.RetryAfter))
			return
		}
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

// GetMessagePayload writes the retained payload byte for byte; sessions with the payload capability or keys with payloads:read.
func (h *Handler) GetMessagePayload(w http.ResponseWriter, r *http.Request, messageID openapi.MessageId) {
	a, err := actorOrKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.deps.Delivery.GetMessagePayload(r.Context(), a, messageID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(p.Body) //nolint:gosec // validated as JSON on accept and served as application/json with nosniff
}

// ListApplicationMessages pages an application's messages.
func (h *Handler) ListApplicationMessages(w http.ResponseWriter, r *http.Request, applicationID openapi.ApplicationId, params openapi.ListApplicationMessagesParams) {
	h.listMessages(w, r, services.ListMessagesInput{ApplicationID: applicationID}, listMessagesFilters{params.Limit, params.Offset, params.Type, params.ExternalId, params.AcceptedAfter, params.AcceptedBefore, (*string)(params.Outcome), (*string)(params.SortBy), (*string)(params.SortDir)})
}

// ListEnvironmentMessages pages an environment's messages across its applications.
func (h *Handler) ListEnvironmentMessages(w http.ResponseWriter, r *http.Request, environmentID openapi.EnvironmentId, params openapi.ListEnvironmentMessagesParams) {
	h.listMessages(w, r, services.ListMessagesInput{EnvironmentID: environmentID}, listMessagesFilters{params.Limit, params.Offset, params.Type, params.ExternalId, params.AcceptedAfter, params.AcceptedBefore, (*string)(params.Outcome), (*string)(params.SortBy), (*string)(params.SortDir)})
}

// listMessagesFilters are the query parameters the two list routes share.
type listMessagesFilters struct {
	limit, offset                 *int
	eventType, externalID         *string
	acceptedAfter, acceptedBefore *time.Time
	outcome, sortBy, sortDir      *string
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request, in services.ListMessagesInput, f listMessagesFilters) {
	p, err := principal(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if p.OrganizationID == "" {
		httpx.WriteError(w, r, apperr.NewForbidden("select a workspace", nil))
		return
	}
	in.Limit = defaultPageSize
	if f.limit != nil {
		in.Limit = *f.limit
	}
	if f.offset != nil {
		in.Offset = *f.offset
	}
	if f.eventType != nil {
		in.Type = *f.eventType
	}
	if f.externalID != nil {
		in.ExternalID = *f.externalID
	}
	in.AcceptedAfter, in.AcceptedBefore = f.acceptedAfter, f.acceptedBefore
	if f.outcome != nil {
		in.Outcome = *f.outcome
	}
	if f.sortBy != nil {
		in.SortBy = *f.sortBy
	}
	if f.sortDir != nil {
		in.SortDir = *f.sortDir
	}
	items, total, err := h.deps.Delivery.ListMessages(r.Context(), p.OrganizationID, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.MessageList{Items: make([]openapi.MessageSummary, 0, len(items)), Total: total}
	for i := range items {
		m := &items[i]
		resp.Items = append(resp.Items, openapi.MessageSummary{
			Message:          toMessageDTO(&m.Message),
			PayloadExpiresAt: m.PayloadExpiresAt,
			DeliveryCounts:   openapi.DeliveryCounts{Queued: m.Counts.Queued, InFlight: m.Counts.InFlight, RetryScheduled: m.Counts.RetryScheduled, Succeeded: m.Counts.Succeeded, Failed: m.Counts.Failed, Cancelled: m.Counts.Cancelled},
		})
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

func toMessageDTO(m *models.Message) openapi.Message {
	return openapi.Message{Id: m.ID, WebhookId: m.WebhookID, ApplicationId: m.ApplicationID, EnvironmentId: m.EnvironmentID, EnvironmentKind: openapi.EnvironmentKind(m.EnvironmentKind), Type: m.Type, ExternalId: m.ExternalID, AcceptedAt: m.AcceptedAt, DeliveryCount: m.DeliveryCount}
}

func toDeliveryDTO(d *models.Delivery) openapi.Delivery {
	return openapi.Delivery{
		Id: d.ID, MessageId: d.MessageID, EndpointId: d.EndpointID, State: openapi.DeliveryState(d.State), AttemptCount: d.AttemptCount,
		DueAt: d.DueAt, DeadlineAt: d.DeadlineAt, FailureReason: d.FailureReason, ReplayOfDeliveryId: d.ReplayOfDeliveryID, CreatedAt: d.CreatedAt, CompletedAt: d.CompletedAt,
	}
}
