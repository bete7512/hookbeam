package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// ListMembers returns the workspace's members with the roles Hookbeam applies.
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	members, err := h.deps.Members.List(r.Context(), a)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.MemberList{Items: make([]openapi.Member, 0, len(members))}
	for i := range members {
		resp.Items = append(resp.Items, toMemberDTO(&members[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// SetMemberRole makes a member a developer or a viewer.
func (h *Handler) SetMemberRole(w http.ResponseWriter, r *http.Request, userID string) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.SetMemberRoleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	m, err := h.deps.Members.SetRole(r.Context(), a, userID, string(req.Role))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toMemberDTO(m))
}

func toMemberDTO(m *models.Member) openapi.Member {
	return openapi.Member{UserId: m.UserID, Role: openapi.MemberRole(m.Role), JoinedAt: m.JoinedAt}
}
