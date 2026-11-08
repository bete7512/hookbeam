package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestRouter(t *testing.T) {
	runEndpoints(t, []endpointCase{
		{"unknown route", http.MethodGet, "/nope", "", nil, http.StatusNotFound, apperr.CodeNotFound, false},
		{"openapi", http.MethodGet, "/openapi.json", "", nil, http.StatusOK, "", false},
		{"docs", http.MethodGet, "/docs", "", nil, http.StatusOK, "", false},
	})
}

const authSpec = `openapi: 3.0.3
info: {title: goauth, version: "1"}
paths:
  /login:
    post:
      operationId: login
      requestBody:
        content:
          application/json:
            schema: {$ref: '#/components/schemas/LoginRequest'}
      responses:
        '200': {description: ok}
components:
  schemas:
    LoginRequest:
      type: object
      properties: {email: {type: string}}
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
`

func TestRouterMountsAuth(t *testing.T) {
	f := newFixtureWithRouter(t, handlers.RouterOptions{
		AuthRoutes: []goauthcfg.RouteInfo{authRoute(http.MethodPost, "/login", http.StatusTeapot)},
		AuthSpec:   []byte(authSpec),
	})

	rec := f.send(httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil))
	assert.Equal(t, http.StatusTeapot, rec.Code, "goauth routes are served as mounted, outside the spec middleware")

	rec = f.send(httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var doc struct {
		Paths      map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas         map[string]json.RawMessage `json:"schemas"`
			SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
		} `json:"components"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Contains(t, doc.Paths, "/v1/auth/login", "goauth paths are merged under the base path")
	assert.Contains(t, doc.Paths, "/v1/me")
	assert.Contains(t, doc.Components.Schemas, "LoginRequest")
	assert.Contains(t, doc.Components.Schemas, "Error", "our components survive the merge")
	assert.Contains(t, doc.Components.SecuritySchemes, "cookieAuth")
}

func TestRouterRejectsCollidingSpecs(t *testing.T) {
	colliding := []byte("openapi: 3.0.3\ninfo: {title: goauth, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    Error: {type: object}\n")
	h, err := handlers.New(handlers.Deps{Auth: &fakeAuth{}, APIKeys: service_mocks.NewMockAPIKeyService(gomock.NewController(t)), DB: pingerFunc(func(context.Context) error { return nil }), Delivery: service_mocks.NewMockDeliveryService(gomock.NewController(t)), DeliveryHealth: pingerFunc(func(context.Context) error { return nil }), Members: service_mocks.NewMockMemberService(gomock.NewController(t)), OrgPolicy: service_mocks.NewMockOrgPolicy(gomock.NewController(t)), Audit: service_mocks.NewMockAuditService(gomock.NewController(t)), Limiter: &fakeLimiter{}, KeySubmitLimit: 10, AccessCookie: accessCookie, Logger: discardLogger()})
	require.NoError(t, err)

	_, err = handlers.NewRouter(h, handlers.RouterOptions{Logger: discardLogger(), MaxBodyBytes: 1 << 20, AuthSpec: colliding, AuthBasePath: authBasePath})

	require.ErrorContains(t, err, `schema "Error" is defined by both specs`)
}
