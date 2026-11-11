package handlers

import (
	"net/http"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// ListNotifications pages the caller's notices in the active workspace, newest first.
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request, params openapi.ListNotificationsParams) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	opts := repos.ListNotificationsOpts{Limit: defaultPageSize}
	if params.Limit != nil {
		opts.Limit = *params.Limit
	}
	if params.Offset != nil {
		opts.Offset = *params.Offset
	}
	if params.Unread != nil {
		opts.UnreadOnly = *params.Unread
	}
	items, total, unread, err := h.deps.Notifications.List(r.Context(), a, opts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resp := openapi.NotificationList{Items: make([]openapi.Notification, 0, len(items)), Total: total, Unread: unread}
	for i := range items {
		resp.Items = append(resp.Items, toNotificationDTO(&items[i]))
	}
	_ = httpx.WriteJSON(w, http.StatusOK, resp)
}

// MarkNotificationRead marks one notice read; reading it again changes nothing.
func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request, notificationID openapi.NotificationId) {
	a, err := actor(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	n, err := h.deps.Notifications.MarkRead(r.Context(), a, notificationID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toNotificationDTO(n))
}

// GetNotificationPreferences returns the caller's alert email choice.
func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	a, err := actorOrKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.deps.Notifications.Preferences(r.Context(), a)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.NotificationPreferences{EmailAlerts: p.EmailAlerts})
}

// UpdateNotificationPreferences stores the caller's alert email choice.
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	a, err := actorOrKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var req openapi.NotificationPreferences
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.deps.Notifications.SetPreferences(r.Context(), a, req.EmailAlerts)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.NotificationPreferences{EmailAlerts: p.EmailAlerts})
}

func toNotificationDTO(n *models.Notification) openapi.Notification {
	return openapi.Notification{
		Id:            n.ID,
		Kind:          openapi.NotificationKind(n.Kind),
		EpisodeId:     n.EpisodeID,
		EndpointId:    n.EndpointID,
		EndpointUrl:   n.EndpointURL,
		ApplicationId: n.ApplicationID,
		Title:         n.Title,
		Body:          n.Body,
		ReadAt:        n.ReadAt,
		CreatedAt:     n.CreatedAt,
	}
}
