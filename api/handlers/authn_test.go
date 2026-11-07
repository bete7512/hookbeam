package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/gen/openapi"
)

// publicOperations is the complete list of operations callable without a token.
// Adding one here is a deliberate, reviewed decision. Signup, login and the other
// account routes are goauth's and are not in this spec.
// Ids are as the embedded spec reports them (oapi-codegen capitalises operationId).
var publicOperations = []string{"GetHealthz", "GetReadyz"}

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
		wantStatus    int
	}{
		{name: "protected route without token", path: "/v1/me", wantStatus: http.StatusUnauthorized},
		{name: "invalid token", path: "/v1/me", authorization: "Bearer bad", wantStatus: http.StatusUnauthorized},
		{name: "bearer token", path: "/v1/me", authorization: "Bearer good", wantStatus: http.StatusOK},
		{name: "token from cookie", path: "/v1/me", cookie: "good", wantStatus: http.StatusOK},
		{name: "public route without token", path: "/healthz", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
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
