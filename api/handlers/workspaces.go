package handlers

import (
	"net/http"
	"time"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// GetWorkspaceLimits reports the active workspace's quotas and usage.
func (h *Handler) GetWorkspaceLimits(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.deps.Delivery.GetWorkspaceLimits(r.Context(), a)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.WorkspaceLimits{}
	resp.Limits.EndpointsPerApplication, resp.Limits.FanoutPerMessage, resp.Limits.BacklogPerWorkspace = l.EndpointsPerApplication, l.FanoutPerMessage, l.BacklogPerWorkspace
	resp.Retention.PayloadSeconds, resp.Retention.IdempotencyWindowSeconds, resp.Retention.AuditSeconds = int64(l.Retention.Payload/time.Second), int64(l.Retention.IdempotencyWindow/time.Second), int64(l.Retention.Audit/time.Second)
	resp.Usage.Backlog = l.Backlog
	resp.Usage.Applications = make([]openapi.ApplicationEndpointCount, 0, len(l.Applications))
	for _, a := range l.Applications {
		resp.Usage.Applications = append(resp.Usage.Applications, openapi.ApplicationEndpointCount{ApplicationId: a.ApplicationID, Name: a.Name, Endpoints: a.Endpoints})
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}
