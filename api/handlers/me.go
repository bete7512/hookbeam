package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// GetMe returns the caller's identity as the control plane sees it.
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	p, err := principal(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	me := openapi.Me{UserId: p.UserID, AuthenticatedAt: p.AuthenticatedAt, ExpiresAt: p.ExpiresAt, Methods: p.Methods}
	if p.OrganizationID != "" {
		me.WorkspaceId = &p.OrganizationID
	}
	_ = httpx.WriteJSON(w, http.StatusOK, me)
}
