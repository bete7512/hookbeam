package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// ListAuditEvents pages the workspace's audit trail across both planes, newest first.
func (h *Handler) ListAuditEvents(w http.ResponseWriter, r *http.Request, params openapi.ListAuditEventsParams) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in := services.ListAuditInput{Limit: defaultPageSize, After: params.After, Before: params.Before}
	if params.Limit != nil {
		in.Limit = *params.Limit
	}
	if params.Offset != nil {
		in.Offset = *params.Offset
	}
	if params.ActorId != nil {
		in.ActorID = *params.ActorId
	}
	if params.ResourceType != nil {
		in.ResourceType = *params.ResourceType
	}
	if params.ResourceId != nil {
		in.ResourceID = *params.ResourceId
	}
	items, total, err := h.deps.Audit.List(r.Context(), a, in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.AuditEventList{Items: make([]openapi.AuditEvent, 0, len(items)), Total: total}
	for i := range items {
		resp.Items = append(resp.Items, toAuditDTO(&items[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// decodeDetails parses the stored JSON object; empty or malformed details show as an empty object.
func decodeDetails(raw []byte) map[string]any {
	details := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &details)
	}
	return details
}

func toAuditDTO(e *models.AuditEvent) openapi.AuditEvent {
	details := decodeDetails(e.Details)
	return openapi.AuditEvent{
		Id:           e.ID,
		Source:       openapi.AuditEventSource(e.Source),
		ActorId:      e.ActorID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceId:   e.ResourceID,
		RequestId:    e.RequestID,
		Details:      details,
		OccurredAt:   e.OccurredAt.UTC(),
	}
}
