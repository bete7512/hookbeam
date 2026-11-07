package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// GetHealthz reports that the process is up. It never checks dependencies.
func (h *Handler) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.Health{Status: openapi.HealthStatusOk})
}

// GetReadyz reports whether the service can take traffic: the database and the delivery service must answer.
func (h *Handler) GetReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.deps.DB.Ping(ctx); err != nil {
		httpx.WriteError(w, r, apperr.NewUnavailable("database unreachable", err))
		return
	}
	if err := h.deps.DeliveryHealth.Ping(ctx); err != nil {
		httpx.WriteError(w, r, apperr.NewUnavailable("delivery service unreachable", err))
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, openapi.Readiness{
		Status: openapi.ReadinessStatusOk,
		Checks: map[string]string{"db": "ok", "delivery": "ok"},
	})
}
