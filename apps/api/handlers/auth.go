package handlers

import (
	"net/http"
	"time"

	"github.com/bete7512/scaffold/apps/api/gen/openapi"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/httpx"
)

// Login starts a session for valid credentials.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req openapi.LoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	pair, err := h.deps.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toTokenResponse(pair))
}

// RefreshToken exchanges a refresh token for new tokens.
func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req openapi.RefreshTokenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	pair, err := h.deps.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = httpx.WriteJSON(w, http.StatusOK, toTokenResponse(pair))
}

// Logout ends the session of a refresh token.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req openapi.RefreshTokenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	if err := h.deps.Auth.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LogoutAll ends every session of the authenticated user.
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
		return
	}

	if err := h.deps.Auth.LogoutAll(r.Context(), claims.UserID); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// VerifyEmail confirms an email address with the token from the verification email.
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req openapi.TokenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.deps.Auth.VerifyEmail(r.Context(), req.Token); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResendVerification sends the verification email again; the answer never reveals whether the address exists.
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req openapi.EmailRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.deps.Auth.RequestVerification(r.Context(), req.Email); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ForgotPassword sends a password reset email; the answer never reveals whether the address exists.
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req openapi.EmailRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.deps.Auth.ForgotPassword(r.Context(), req.Email); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ResetPassword sets a new password with the token from the reset email.
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req openapi.ResetPasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.deps.Auth.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toTokenResponse(pair *models.TokenPair) openapi.TokenResponse {
	return openapi.TokenResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    openapi.TokenResponseTokenType("Bearer"),
		ExpiresIn:    int(time.Until(pair.AccessTokenExpiresAt).Round(time.Second).Seconds()),
	}
}
