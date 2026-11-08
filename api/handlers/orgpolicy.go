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
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		route.Handler.ServeHTTP(rec, r)
		if rec.status < 300 {
			// goauth committed the change in its own transaction; the audit row follows as closely as it can.
			if err := h.deps.Audit.Record(r.Context(), actor, orgAuditActions[action], "member", 0, orgAuditDetails(target)); err != nil {
				h.deps.Logger.WarnContext(r.Context(), "audit of organization action failed", "action", string(action), "error", err)
			}
		}
	})
}

var orgAuditActions = map[services.OrgAction]string{
	services.OrgInvite:       services.AuditOrgInvite,
	services.OrgChangeRole:   services.AuditOrgChangeRole,
	services.OrgRemoveMember: services.AuditOrgRemove,
}

// statusRecorder remembers the status goauth wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// orgAuditDetails keeps only the target fields the action carried.
func orgAuditDetails(t services.OrgTarget) map[string]any {
	details := map[string]any{}
	if t.UserID != "" {
		details["user_id"] = t.UserID
	}
	if t.Email != "" {
		details["email"] = t.Email
	}
	if t.Role != "" {
		details["role"] = t.Role
	}
	return details
}
