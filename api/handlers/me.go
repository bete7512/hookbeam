package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// GetMe returns the caller's identity as the control plane sees it: a user session or an API key.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	me := openapi.Me{AuthenticatedAt: p.AuthenticatedAt, Methods: p.Methods}
	if !p.ExpiresAt.IsZero() {
		me.ExpiresAt = &p.ExpiresAt
	}
	if p.UserID != "" {
		me.UserId = &p.UserID
	}
	if p.OrganizationID != "" {
		me.WorkspaceId = &p.OrganizationID
	}
	_ = httpx.WriteJSON(w, http.StatusOK, me)
}
