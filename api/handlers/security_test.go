//go:build security

package handlers_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// TestSecurityCSRFOriginOnCookieRoutes: a state-changing request that rides on the session cookie is accepted only
// from the console's origin; bearer requests and reads are not the browser's ambient authority.
func TestSecurityCSRFOriginOnCookieRoutes(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		origin     string
		referer    string
		bearer     bool
		wantStatus int
	}{
		{name: "cookie POST without Origin is refused", method: http.MethodPost, path: "/v1/applications", wantStatus: http.StatusForbidden},
		{name: "cookie POST from another origin is refused", method: http.MethodPost, path: "/v1/applications", origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "cookie POST from the console passes", method: http.MethodPost, path: "/v1/applications", origin: consoleOrigin, wantStatus: http.StatusCreated},
		{name: "cookie POST with the console Referer passes", method: http.MethodPost, path: "/v1/applications", referer: consoleOrigin + "/applications", wantStatus: http.StatusCreated},
		{name: "cookie DELETE from another origin is refused", method: http.MethodDelete, path: "/v1/applications/1", origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "cookie GET needs no Origin", method: http.MethodGet, path: "/v1/me", wantStatus: http.StatusOK},
		{name: "bearer POST needs no Origin", method: http.MethodPost, path: "/v1/applications", bearer: true, wantStatus: http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.wantStatus == http.StatusCreated {
				f.delivery.EXPECT().CreateApplication(gomock.Any(), adaActor, "Customer A").Return(&models.Application{ID: 1, Name: "Customer A", CreatedAt: ada.AuthenticatedAt}, nil)
			}
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"name":"Customer A"}`))
			req.Header.Set("Content-Type", "application/json")
			if tt.bearer {
				req.Header.Set("Authorization", "Bearer good")
			} else {
				req.AddCookie(&http.Cookie{Name: accessCookie, Value: "good"})
			}
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}
			rec := f.send(req)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantStatus == http.StatusForbidden {
				require.Contains(t, rec.Body.String(), "cross-site request refused")
			}
		})
	}
}

// TestSecurityCredentialKinds: a session cannot act as a sender and a key cannot act as a person.
func TestSecurityCredentialKinds(t *testing.T) {
	t.Run("a session cannot submit events", func(t *testing.T) {
		f := newFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/v1/applications/7/messages", strings.NewReader(`{"type":"a"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer good")
		req.Header.Set("Idempotency-Key", "k")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
	t.Run("a key cannot manage endpoints, members or keys", func(t *testing.T) {
		for _, path := range []string{"/v1/applications", "/v1/api-keys", "/v1/environments"} {
			f := newFixture(t)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"name":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer hb_sender")
			rec := f.send(req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, "%s: %s", path, rec.Body.String())
		}
	})
	t.Run("a key without the scope is refused before the data plane", func(t *testing.T) {
		f := newFixture(t)
		expectSenderKey(f, models.ScopeMessagesRead)
		rec := submit(f, "k", submission)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "api key lacks scope messages:write")
	})
}

// TestSecurityErrorBodiesLeakNothing: an internal failure answers a generic message, never the driver's text.
func TestSecurityErrorBodiesLeakNothing(t *testing.T) {
	f := newFixture(t)
	f.delivery.EXPECT().GetApplication(gomock.Any(), adaActor, int64(1)).Return(nil, apperr.NewInternal(errors.New("pq: password authentication failed for user hookbeam password=hunter2 host=db.internal")))
	rec := f.do(http.MethodGet, "/v1/applications/1", "")
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	for _, leak := range []string{"hunter2", "db.internal", "pq:"} {
		require.NotContains(t, rec.Body.String(), leak)
	}
	require.Contains(t, rec.Body.String(), apperr.CodeInternal)
	require.Contains(t, f.logs.String(), "hunter2", "the operator's log keeps the cause")
}
