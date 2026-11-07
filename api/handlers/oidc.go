package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/oidc"
)

const (
	googleProvider = "google"
	oidcCookie     = "oidc_flow"
	oidcFlowTTL    = 10 * time.Minute
)

// GoogleLogin redirects to Google; state and the PKCE verifier wait in an HttpOnly cookie for the callback.
func (h *Handler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	if h.deps.Google == nil {
		httpx.WriteError(w, r, apperr.NewUnavailable("google login is not configured", nil))
		return
	}
	state, err := oidc.NewState()
	if err != nil {
		httpx.WriteError(w, r, apperr.NewInternal(err))
		return
	}
	verifier, err := oidc.NewVerifier()
	if err != nil {
		httpx.WriteError(w, r, apperr.NewInternal(err))
		return
	}
	http.SetCookie(w, flowCookie(state+"."+verifier, int(oidcFlowTTL.Seconds())))
	http.Redirect(w, r, h.deps.Google.AuthCodeURL(state, verifier), http.StatusFound)
}

// GoogleCallback finishes the login Google redirected back from and starts a session.
func (h *Handler) GoogleCallback(w http.ResponseWriter, r *http.Request, params openapi.GoogleCallbackParams) {
	if h.deps.Google == nil {
		httpx.WriteError(w, r, apperr.NewUnavailable("google login is not configured", nil))
		return
	}
	cookie, err := r.Cookie(oidcCookie)
	if err != nil {
		httpx.WriteError(w, r, apperr.NewBadRequest("login flow expired or not started", err))
		return
	}
	state, verifier, ok := strings.Cut(cookie.Value, ".")
	if !ok || state != params.State {
		httpx.WriteError(w, r, apperr.NewBadRequest("state mismatch", nil))
		return
	}
	http.SetCookie(w, flowCookie("", -1))

	identity, err := h.deps.Google.Exchange(r.Context(), params.Code, verifier)
	if err != nil {
		httpx.WriteError(w, r, apperr.NewUnauthorized("google login failed", err))
		return
	}
	pair, err := h.deps.Auth.LoginWithIdentity(r.Context(), googleProvider, identity)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toTokenResponse(pair))
}

// flowCookie carries the login state between the redirect and the callback. Secure is fine on localhost: browsers treat it as a secure context.
func flowCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: oidcCookie, Value: value, Path: "/v1/auth/google", MaxAge: maxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
}
