package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/scaffold/apps/api/gen/openapi"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
)

// publicOperations is the complete list of operations callable without a token.
// Adding one here is a deliberate, reviewed decision.
// Ids are as the embedded spec reports them (oapi-codegen capitalises operationId).
var publicOperations = []string{"GetHealthz", "GetReadyz", "CreateUser", "Login", "RefreshToken", "Logout", "VerifyEmail", "ResendVerification", "ForgotPassword", "ResetPassword", "GoogleLogin", "GoogleCallback"}

func TestPublicOperationsArePinned(t *testing.T) {
	doc, err := openapi.GetSpec()
	require.NoError(t, err)

	var public []string
	for _, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if op.Security != nil && len(*op.Security) == 0 {
				public = append(public, op.OperationID)
			}
		}
	}
	slices.Sort(public)
	want := slices.Clone(publicOperations)
	slices.Sort(want)
	assert.Equal(t, want, public, "the set of public operations changed; update publicOperations only on purpose")
}

func TestAuthentication(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		authorization string
		cookie        string
		setup         func(f *fixture)
		wantStatus    int
	}{
		{name: "protected route without token", path: "/v1/me", wantStatus: http.StatusUnauthorized},
		{
			name: "invalid token", path: "/v1/me", authorization: "Bearer bad", wantStatus: http.StatusUnauthorized,
			setup: func(f *fixture) {
				f.auth.EXPECT().Authenticate(gomock.Any(), "bad").Return(auth.Claims{}, apperr.NewUnauthorized("invalid or expired token", nil))
			},
		},
		{
			name: "token from cookie", path: "/v1/me", cookie: "good", wantStatus: http.StatusOK,
			setup: func(f *fixture) { f.users.EXPECT().GetUserFor(gomock.Any(), ada.ID, ada.ID).Return(&ada, nil) },
		},
		{name: "public route without token", path: "/healthz", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "access_token", Value: tt.cookie})
			}

			rec := f.send(req)

			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}
