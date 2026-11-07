package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/apps/api/handlers"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/oidc"
)

// fakeProvider records the login it started and returns a fixed identity on the matching verifier.
type fakeProvider struct {
	state, verifier string
	exchangeErr     error
}

func (p *fakeProvider) AuthCodeURL(state, verifier string) string {
	p.state, p.verifier = state, verifier
	return "https://accounts.google.test/auth?state=" + url.QueryEscape(state)
}

func (p *fakeProvider) Exchange(_ context.Context, code, verifier string) (oidc.Identity, error) {
	if p.exchangeErr != nil {
		return oidc.Identity{}, p.exchangeErr
	}
	if code != "good-code" || verifier != p.verifier {
		return oidc.Identity{}, errors.New("bad code or verifier")
	}
	return oidc.Identity{Subject: "sub-1", Email: "ada@example.com", EmailVerified: true, Name: "Ada"}, nil
}

func TestGoogleLogin(t *testing.T) {
	f := newFixture(t)

	rec := f.send(httptest.NewRequest(http.MethodGet, "/v1/auth/google", nil))

	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	assert.Equal(t, "https://accounts.google.test/auth?state="+url.QueryEscape(f.google.state), rec.Header().Get("Location"))
	cookie := findCookie(rec, "oidc_flow")
	require.NotNil(t, cookie)
	assert.Equal(t, f.google.state+"."+f.google.verifier, cookie.Value)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, "/v1/auth/google", cookie.Path)
}

func TestGoogleCallback(t *testing.T) {
	pair := &models.TokenPair{AccessToken: "a", RefreshToken: "r", AccessTokenExpiresAt: time.Now().Add(15 * time.Minute)}
	tests := []struct {
		name       string
		query      string // built after the login step; {state} is replaced
		cookie     bool
		setup      func(f *fixture)
		wantStatus int
		wantError  string
	}{
		{
			name: "links and logs in", query: "code=good-code&state={state}", cookie: true,
			setup: func(f *fixture) {
				f.auth.EXPECT().LoginWithIdentity(gomock.Any(), "google", oidc.Identity{Subject: "sub-1", Email: "ada@example.com", EmailVerified: true, Name: "Ada"}).Return(pair, nil)
			},
			wantStatus: http.StatusOK,
		},
		{name: "missing cookie", query: "code=good-code&state={state}", wantStatus: http.StatusBadRequest, wantError: apperr.CodeBadRequest},
		{name: "state mismatch", query: "code=good-code&state=other", cookie: true, wantStatus: http.StatusBadRequest, wantError: apperr.CodeBadRequest},
		{name: "missing code", query: "state={state}", cookie: true, wantStatus: http.StatusBadRequest, wantError: apperr.CodeBadRequest},
		{
			name: "exchange rejected", query: "code=good-code&state={state}", cookie: true,
			setup:      func(f *fixture) { f.google.exchangeErr = oidc.ErrInvalidToken },
			wantStatus: http.StatusUnauthorized, wantError: apperr.CodeUnauthorized,
		},
		{
			name: "service refuses the identity", query: "code=good-code&state={state}", cookie: true,
			setup: func(f *fixture) {
				f.auth.EXPECT().LoginWithIdentity(gomock.Any(), "google", gomock.Any()).Return(nil, apperr.NewUnauthorized("email not verified by the provider", nil))
			},
			wantStatus: http.StatusUnauthorized, wantError: apperr.CodeUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			start := f.send(httptest.NewRequest(http.MethodGet, "/v1/auth/google", nil))
			require.Equal(t, http.StatusFound, start.Code)
			req := httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?"+replaceState(tt.query, f.google.state), nil)
			if tt.cookie {
				req.AddCookie(findCookie(start, "oidc_flow"))
			}

			rec := f.send(req)

			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantError != "" {
				assert.Equal(t, tt.wantError, errorCode(t, rec))
			}
			if tt.cookie && tt.wantStatus != http.StatusBadRequest {
				cleared := findCookie(rec, "oidc_flow")
				require.NotNil(t, cleared, "flow cookie is cleared after the callback")
				assert.Equal(t, -1, cleared.MaxAge)
			}
		})
	}
}

func TestGoogleDisabled(t *testing.T) {
	f := newFixture(t)
	h, err := handlers.New(handlers.Deps{Users: f.users, Auth: f.auth, DB: pingerFunc(func(context.Context) error { return nil }), Limiter: newMemoryLimiter(t), RateLimit: rateLimit, Logger: discardLogger()})
	require.NoError(t, err)
	router, err := handlers.NewRouter(h, handlers.RouterOptions{Logger: discardLogger(), MaxBodyBytes: 1 << 20})
	require.NoError(t, err)

	for _, path := range []string{"/v1/auth/google", "/v1/auth/google/callback?code=c&state=s"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
	}
}

func replaceState(query, state string) string {
	u, _ := url.ParseQuery(query)
	if u.Get("state") == "{state}" {
		u.Set("state", state)
	}
	return u.Encode()
}

func findCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
