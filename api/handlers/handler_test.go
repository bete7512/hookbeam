package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
)

const (
	accessCookie = "goauth_access_test"
	authBasePath = "/v1/auth"
)

// workspace is ada's active workspace in every fixture.
const workspace = "ws_1"

var ada = types.Principal{
	UserID:          "u_ada",
	OrganizationID:  workspace,
	Methods:         []string{"password"},
	AuthenticatedAt: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
	ExpiresAt:       time.Date(2026, 11, 7, 12, 15, 0, 0, time.UTC),
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

// fakeAuth accepts the token "good" as ada and rejects everything else.
type fakeAuth struct {
	principal types.Principal
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (types.Principal, error) {
	if token == "good" {
		return f.principal, nil
	}
	return types.Principal{}, errors.New("invalid token")
}

type fixture struct {
	auth        *fakeAuth
	apiKeys     *service_mocks.MockAPIKeyService
	dbErr       error
	deliveryErr error
	handler     http.Handler
	logs        bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWithRouter(t, handlers.RouterOptions{})
}

// newFixtureWithRouter builds the router with extra options (goauth routes and spec).
func newFixtureWithRouter(t *testing.T, o handlers.RouterOptions) *fixture {
	t.Helper()
	f := &fixture{auth: &fakeAuth{principal: ada}, apiKeys: service_mocks.NewMockAPIKeyService(gomock.NewController(t))}
	l := logger.New(logger.Options{Level: "info", Format: "json", Writer: &f.logs})
	h, err := handlers.New(handlers.Deps{
		Auth:         f.auth,
		APIKeys:      f.apiKeys,
		DB:           pingerFunc(func(context.Context) error { return f.dbErr }),
		Delivery:     pingerFunc(func(context.Context) error { return f.deliveryErr }),
		AccessCookie: accessCookie,
		Logger:       l,
	})
	require.NoError(t, err)
	o.Logger, o.MaxBodyBytes, o.AuthBasePath = l, 1<<20, authBasePath
	f.handler, err = handlers.NewRouter(h, o)
	require.NoError(t, err)
	return f
}

// do sends a request authenticated with the "good" token.
func (f *fixture) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer good")
	return f.send(req)
}

func (f *fixture) send(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// endpointCase is one request through the real router.
type endpointCase struct {
	name       string
	method     string
	path       string
	body       string
	setup      func(f *fixture)
	wantStatus int
	wantError  string
	auth       bool // protected route: the same request without a token must get 401
}

// runEndpoints runs each case; protected cases are first sent without a token and must get 401.
func runEndpoints(t *testing.T, cases []endpointCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if tt.auth {
				req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				req.Header.Set("Content-Type", "application/json")
				rec := newFixture(t).send(req)
				require.Equal(t, http.StatusUnauthorized, rec.Code, "without a token: %s", rec.Body.String())
			}

			f := newFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}

			rec := f.do(tt.method, tt.path, tt.body)

			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantError != "" {
				var body httpx.ErrorResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tt.wantError, body.Error)
				assert.NotEmpty(t, body.RequestID)
			}
		})
	}
}

func TestNew(t *testing.T) {
	_, err := handlers.New(handlers.Deps{})
	require.Error(t, err)
}

// authRoute is a goauth-style route for router tests.
func authRoute(method, path string, status int) goauthcfg.RouteInfo {
	return goauthcfg.RouteInfo{Name: "test." + path, Method: method, Path: authBasePath + path, Handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }}
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
