package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// publicOperations is the complete list of operations callable without a token.
// Adding one here is a deliberate, reviewed decision. Signup, login and the other
// account routes are goauth's and are not in this spec.
// Ids are as the embedded spec reports them (oapi-codegen capitalises operationId).
var publicOperations = []string{"GetHealthz", "GetReadyz"}

// apiKeyOperations is the complete list of operations an API key may call.
var apiKeyOperations = []string{"GetMe", "GetMessage", "GetMessagePayload", "ListApplicationMessages", "ListDeliveryAttempts", "ListEnvironmentMessages", "ReplayDelivery", "SubmitMessage"}

func TestPublicOperationsArePinned(t *testing.T) {
	doc, err := openapi.GetSpec()
	require.NoError(t, err)

	var public, keyed []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security == nil {
				continue
			}
			if len(*op.Security) == 0 {
				public = append(public, op.OperationID)
			}
			for _, req := range *op.Security {
				if _, ok := req["apiKeyAuth"]; ok {
					keyed = append(keyed, op.OperationID)
				}
			}
		}
	}
	slices.Sort(public)
	slices.Sort(keyed)
	want := slices.Clone(publicOperations)
	slices.Sort(want)
	assert.Equal(t, want, public, "the set of public operations changed; update publicOperations only on purpose")
	wantKeyed := slices.Clone(apiKeyOperations)
	slices.Sort(wantKeyed)
	assert.Equal(t, wantKeyed, keyed, "the set of api-key operations changed; update apiKeyOperations only on purpose")
}

func TestAuthentication(t *testing.T) {
	keyPrincipal := types.Principal{OrganizationID: "ws_key", Methods: []string{"api_key"}}
	tests := []struct {
		name          string
		path          string
		method        string
		headers       map[string]string
		authorization string
		cookie        string
		setup         func(f *fixture)
		wantStatus    int
	}{
		{name: "protected route without token", path: "/v1/me", wantStatus: http.StatusUnauthorized},
		{name: "invalid token", path: "/v1/me", authorization: "Bearer bad", wantStatus: http.StatusUnauthorized},
		{name: "bearer token", path: "/v1/me", authorization: "Bearer good", wantStatus: http.StatusOK},
		{name: "token from cookie", path: "/v1/me", cookie: "good", wantStatus: http.StatusOK},
		{name: "public route without token", path: "/healthz", wantStatus: http.StatusOK},
		{
			name: "api key on a route that accepts keys", path: "/v1/me", authorization: "Bearer hb_good", wantStatus: http.StatusOK,
			setup: func(f *fixture) {
				f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_good").Return(keyPrincipal, &models.APIKey{ID: 9, Scopes: []string{models.ScopeMessagesWrite}}, nil)
			},
		},
		{
			name: "invalid api key", path: "/v1/me", authorization: "Bearer hb_bad", wantStatus: http.StatusUnauthorized,
			setup: func(f *fixture) {
				f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_bad").Return(types.Principal{}, nil, apperr.NewUnauthorized("invalid api key", nil))
			},
		},
		{name: "api key on a session-only route", path: "/v1/api-keys", authorization: "Bearer hb_good", wantStatus: http.StatusUnauthorized},
		{name: "session on a key-only route", path: "/v1/applications/7/messages", method: http.MethodPost, headers: map[string]string{"Idempotency-Key": "k"}, authorization: "Bearer good", wantStatus: http.StatusUnauthorized},
		{
			name: "api key lacking the operation's scope", path: "/v1/messages/1", authorization: "Bearer hb_write", wantStatus: http.StatusForbidden,
			setup: func(f *fixture) {
				f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_write").Return(keyPrincipal, &models.APIKey{ID: 9, Scopes: []string{models.ScopeMessagesWrite}}, nil)
			},
		},
		{
			name: "api key holding the operation's scope", path: "/v1/messages/1", authorization: "Bearer hb_read", wantStatus: http.StatusOK,
			setup: func(f *fixture) {
				f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_read").Return(keyPrincipal, &models.APIKey{ID: 9, Scopes: []string{models.ScopeMessagesRead}}, nil)
				f.delivery.EXPECT().GetMessage(gomock.Any(), "ws_key", int64(1)).Return(&models.Message{ID: 1}, nil, nil)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: accessCookie, Value: tt.cookie})
			}

			rec := f.send(req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}
