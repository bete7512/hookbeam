package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		method     string
		ips        []string // RemoteAddr per request, in order
		wantCodes  []int
		trustProxy bool
		forwarded  string // X-Forwarded-For on every request
	}{
		{
			name: "limit per ip on login", path: "/v1/auth/login", method: http.MethodPost,
			ips: []string{"10.0.0.1:1", "10.0.0.1:2", "10.0.0.1:3"}, wantCodes: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests},
		},
		{
			name: "other ips are not affected", path: "/v1/auth/login", method: http.MethodPost,
			ips: []string{"10.0.0.1:1", "10.0.0.1:2", "10.0.0.2:1"}, wantCodes: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusUnauthorized},
		},
		{
			name: "registration is limited", path: "/v1/users", method: http.MethodPost,
			ips: []string{"10.0.0.1:1", "10.0.0.1:2", "10.0.0.1:3"}, wantCodes: []int{http.StatusUnprocessableEntity, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
		},
		{
			name: "health is not limited", path: "/healthz", method: http.MethodGet,
			ips: []string{"10.0.0.1:1", "10.0.0.1:2", "10.0.0.1:3"}, wantCodes: []int{http.StatusOK, http.StatusOK, http.StatusOK},
		},
		{
			name: "forwarded header ignored unless the proxy is trusted", path: "/v1/auth/login", method: http.MethodPost, forwarded: "1.1.1.1, 2.2.2.2",
			ips: []string{"10.0.0.1:1", "10.0.0.1:2", "10.0.0.1:3"}, wantCodes: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests},
		},
		{
			name: "rightmost forwarded address counts behind a trusted proxy", path: "/v1/auth/login", method: http.MethodPost, trustProxy: true, forwarded: "1.1.1.1, 2.2.2.2",
			ips: []string{"10.0.0.1:1", "10.0.0.2:1", "10.0.0.3:1"}, wantCodes: []int{http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixtureWithRateLimit(t, 2, time.Minute, tt.trustProxy)
			f.auth.EXPECT().Login(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, apperr.NewUnauthorized("invalid email or password", nil)).AnyTimes()
			f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).Return(apperr.NewValidation(map[string]string{"name": "is required"})).AnyTimes()

			for i, ip := range tt.ips {
				req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"email":"ada@example.com","password":"correct horse battery"}`))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = ip
				if tt.forwarded != "" {
					req.Header.Set("X-Forwarded-For", tt.forwarded)
				}

				rec := f.send(req)

				require.Equal(t, tt.wantCodes[i], rec.Code, "request %d: %s", i, rec.Body.String())
				if rec.Code == http.StatusTooManyRequests {
					assert.Equal(t, apperr.CodeRateLimited, errorCode(t, rec))
					assert.NotEmpty(t, rec.Header().Get("Retry-After"))
				}
			}
		})
	}
}

func TestRateLimitPoliciesAreIndependent(t *testing.T) {
	f := newFixtureWithRateLimit(t, 1, time.Minute, false)
	f.auth.EXPECT().Login(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, apperr.NewUnauthorized("invalid email or password", nil))
	f.users.EXPECT().Register(gomock.Any(), gomock.Any(), gomock.Any()).Return(apperr.NewValidation(map[string]string{"name": "is required"}))
	f.auth.EXPECT().ForgotPassword(gomock.Any(), gomock.Any()).Return(nil)
	f.auth.EXPECT().Logout(gomock.Any(), gomock.Any()).Return(nil)
	for _, tt := range []struct {
		path string
		body string
		want int
	}{
		{"/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`, http.StatusUnauthorized},
		{"/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`, http.StatusTooManyRequests},
		{"/v1/users", `{"email":"ada@example.com","password":"wrong"}`, http.StatusUnprocessableEntity},
		{"/v1/auth/forgot-password", `{"email":"ada@example.com"}`, http.StatusAccepted},
		{"/v1/auth/logout", `{"refreshToken":"refresh"}`, http.StatusNoContent},
	} {
		req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.1:1234"
		res := f.send(req)
		require.Equal(t, tt.want, res.Code, "%s: %s", tt.path, res.Body.String())
	}
}
