package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/auth"
	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
)

var ada = models.User{
	ID:        1,
	Email:     "ada@example.com",
	Name:      "Ada",
	CreatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	UpdatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

type fixture struct {
	users   *service_mocks.MockUserService
	auth    *service_mocks.MockAuthService
	google  *fakeProvider
	dbErr   error
	handler http.Handler
	logs    bytes.Buffer
}

// rateLimit is generous so endpoint tests never trip it; TestRateLimit builds its own fixture.
var rateLimit = handlers.RateLimit{Limit: 1000, Window: time.Minute}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureWithRateLimit(t, rateLimit.Limit, rateLimit.Window, false)
}

func newFixtureWithRateLimit(t *testing.T, limit int, window time.Duration, trustProxy bool) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{users: service_mocks.NewMockUserService(ctrl), auth: service_mocks.NewMockAuthService(ctrl), google: &fakeProvider{}}
	f.auth.EXPECT().Authenticate(gomock.Any(), "good").Return(auth.Claims{UserID: ada.ID}, nil).AnyTimes()
	l := logger.New(logger.Options{Level: "info", Format: "json", Writer: &f.logs})
	h, err := handlers.New(handlers.Deps{
		Users:     f.users,
		Auth:      f.auth,
		DB:        pingerFunc(func(context.Context) error { return f.dbErr }),
		Limiter:   newMemoryLimiter(t),
		RateLimit: handlers.RateLimit{Limit: limit, Window: window, TrustProxy: trustProxy},
		Google:    f.google,
		Logger:    l,
	})
	require.NoError(t, err)
	f.handler, err = handlers.NewRouter(h, handlers.RouterOptions{Logger: l, MaxBodyBytes: 1 << 20})
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

// endpointCase is one request through the real router with mocked services.
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
				rec := newFixture(t).send(req) // no expectations: the handler must not run
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

// errorCode reads the api error code from an error response.
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body httpx.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Error
}

// newMemoryLimiter returns a limiter over a cache that is closed when the test ends.
func newMemoryLimiter(t *testing.T) *ratelimitmemory.Limiter {
	t.Helper()
	c, err := cache.New(cache.Config{MaxItems: 1000})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return ratelimitmemory.New(c)
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
