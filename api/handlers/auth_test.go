package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestAuth(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"login", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"correct horse battery"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "correct horse battery").
				Return(&models.TokenPair{AccessToken: "a", RefreshToken: "r", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, "", false},
		{"login invalid credentials", http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`, func(f *fixture) {
			f.auth.EXPECT().Login(gomock.Any(), "ada@example.com", "wrong").Return(nil, apperr.NewUnauthorized("invalid email or password", nil))
		}, http.StatusUnauthorized, apperr.CodeUnauthorized, false},
		{"refresh", http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Refresh(gomock.Any(), "r").
				Return(&models.TokenPair{AccessToken: "a2", RefreshToken: "r2", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}, nil)
		}, http.StatusOK, "", false},
		{"logout", http.MethodPost, "/v1/auth/logout", `{"refreshToken":"r"}`, func(f *fixture) {
			f.auth.EXPECT().Logout(gomock.Any(), "r").Return(nil)
		}, http.StatusNoContent, "", false},
		{"logout all", http.MethodPost, "/v1/auth/logout-all", "", func(f *fixture) {
			f.auth.EXPECT().LogoutAll(gomock.Any(), ada.ID).Return(nil)
		}, http.StatusNoContent, "", true},
		{"verify email", http.MethodPost, "/v1/auth/verify-email", `{"token":"t"}`, func(f *fixture) {
			f.auth.EXPECT().VerifyEmail(gomock.Any(), "t").Return(nil)
		}, http.StatusNoContent, "", false},
		{"verify email bad token", http.MethodPost, "/v1/auth/verify-email", `{"token":"t"}`, func(f *fixture) {
			f.auth.EXPECT().VerifyEmail(gomock.Any(), "t").Return(apperr.NewUnauthorized("invalid or expired token", nil))
		}, http.StatusUnauthorized, apperr.CodeUnauthorized, false},
		{"resend verification", http.MethodPost, "/v1/auth/resend-verification", `{"email":"ada@example.com"}`, func(f *fixture) {
			f.auth.EXPECT().RequestVerification(gomock.Any(), "ada@example.com").Return(nil)
		}, http.StatusAccepted, "", false},
		{"forgot password", http.MethodPost, "/v1/auth/forgot-password", `{"email":"ada@example.com"}`, func(f *fixture) {
			f.auth.EXPECT().ForgotPassword(gomock.Any(), "ada@example.com").Return(nil)
		}, http.StatusAccepted, "", false},
		{"forgot password malformed body", http.MethodPost, "/v1/auth/forgot-password", `{"email":`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"reset password", http.MethodPost, "/v1/auth/reset-password", `{"token":"t","newPassword":"correct horse battery"}`, func(f *fixture) {
			f.auth.EXPECT().ResetPassword(gomock.Any(), "t", "correct horse battery").Return(nil)
		}, http.StatusNoContent, "", false},
		{"reset password weak", http.MethodPost, "/v1/auth/reset-password", `{"token":"t","newPassword":"short"}`, func(f *fixture) {
			f.auth.EXPECT().ResetPassword(gomock.Any(), "t", "short").Return(apperr.NewValidation(map[string]string{"newPassword": "too short"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, false},
	})
}
