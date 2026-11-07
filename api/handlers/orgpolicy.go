package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	goauthcfg "github.com/bete7512/goauth/pkg/config"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// guardedOrgRoutes maps goauth route names to the action OrgPolicy checks before goauth handles them.
var guardedOrgRoutes = map[string]services.OrgAction{
	"org.invite":         services.OrgInvite,
	"org.members.update": services.OrgChangeRole,
	"org.members.remove": services.OrgRemoveMember,
}

// guardOrgRoute wraps one of goauth's organization routes with the membership policy; other routes pass through.
func (h *Handler) guardOrgRoute(route goauthcfg.RouteInfo) http.Handler {
	action, guarded := guardedOrgRoutes[route.Name]
	if !guarded {
		return route.Handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := h.accessToken(r)
		if token == "" || strings.HasPrefix(token, models.APIKeyPrefix) {
			httpx.WriteError(w, r, apperr.NewUnauthorized("a user session is required", nil))
			return
		}
		p, err := h.deps.Auth.Authenticate(r.Context(), token)
		if err != nil {
			httpx.WriteError(w, r, apperr.NewUnauthorized("invalid or expired token", err))
			return
		}
		actor := services.Actor{UserID: p.UserID, WorkspaceID: r.PathValue("orgId")}
		target := services.OrgTarget{UserID: r.PathValue("userId")}
		if action != services.OrgRemoveMember {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				httpx.WriteError(w, r, apperr.NewBadRequest("unreadable body", err))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var fields struct {
				Email string `json:"email"`
				Role  string `json:"role"`
			}
			if len(bytes.TrimSpace(body)) > 0 {
				if err := json.Unmarshal(body, &fields); err != nil {
					httpx.WriteError(w, r, apperr.NewBadRequest("invalid JSON body", err))
					return
				}
			}
			target.Email, target.Role = fields.Email, fields.Role
		}
		if err := h.deps.OrgPolicy.Check(r.Context(), actor, action, target); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		route.Handler.ServeHTTP(w, r)
	})
}
