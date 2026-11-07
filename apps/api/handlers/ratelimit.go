package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/bete7512/scaffold/apps/api/gen/openapi"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/httpx"
	"github.com/bete7512/scaffold/pkg/ratelimit"
)

func (h *Handler) ratePolicy(pattern string) (scope string, limit int, window time.Duration) {
	cfg := h.deps.RateLimit
	switch pattern {
	case "POST /v1/auth/login":
		return "login", cfg.Limit, cfg.Window
	case "GET /v1/auth/google", "GET /v1/auth/google/callback":
		return "oidc", cfg.Limit, cfg.Window
	case "POST /v1/users":
		return "register", cfg.RegisterLimit, cfg.RegisterWindow
	case "POST /v1/auth/forgot-password", "POST /v1/auth/resend-verification":
		return "recovery", cfg.RecoveryLimit, cfg.RecoveryWindow
	case "POST /v1/auth/reset-password", "POST /v1/auth/verify-email":
		return "action", cfg.RecoveryLimit, cfg.RecoveryWindow
	case "POST /v1/auth/refresh", "POST /v1/auth/logout", "POST /v1/auth/logout-all", "PUT /v1/users/{id}/password":
		return "session", cfg.SessionLimit, cfg.SessionWindow
	default:
		return "", 0, 0
	}
}

// rateLimit applies independent per-IP budgets to authentication operation families.
func (h *Handler) rateLimit() openapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, limit, window := h.ratePolicy(r.Pattern)
			if scope == "" {
				next.ServeHTTP(w, r)
				return
			}
			key := "ip:" + scope + ":" + httpx.ClientIP(r, h.deps.RateLimit.TrustProxy)
			decision, err := h.deps.Limiter.Allow(r.Context(), key, limit, window)
			if err != nil {
				if errors.Is(err, ratelimit.ErrUnavailable) {
					httpx.WriteError(w, r, apperr.NewUnavailable("rate limiter unavailable", err))
				} else {
					httpx.WriteError(w, r, apperr.NewInternal(err))
				}
				return
			}
			if !decision.Allowed {
				httpx.WriteError(w, r, apperr.NewTooManyRequests(decision.RetryAfter))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
