// Package handlers implements the generated openapi.ServerInterface.
// Handlers decode requests into generated DTOs, call services, and write
// responses; every error goes through httpx.WriteError.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/oidc"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RateLimit configures independent per-client-IP authentication budgets.
type RateLimit struct {
	RegisterLimit  int
	RegisterWindow time.Duration
	RecoveryLimit  int
	RecoveryWindow time.Duration
	SessionLimit   int
	SessionWindow  time.Duration
	Limit          int
	Window         time.Duration
	TrustProxy     bool // take the client IP from X-Forwarded-For (behind a load balancer)
}

// Deps are the handler dependencies; all are required except Google, which is nil when Google login is not configured.
type Deps struct {
	Users     services.UserService
	Auth      services.AuthService
	DB        Pinger
	Limiter   ratelimit.Limiter
	RateLimit RateLimit
	Google    oidc.Provider
	Logger    *slog.Logger
}

// Handler serves every operation in the OpenAPI spec.
type Handler struct {
	deps Deps
}

var _ openapi.ServerInterface = (*Handler)(nil)

// New returns a Handler or an error if a dependency is missing.
func New(deps Deps) (*Handler, error) {
	if deps.Users == nil || deps.Auth == nil || deps.DB == nil || deps.Limiter == nil || deps.Logger == nil || deps.RateLimit.Limit <= 0 || deps.RateLimit.Window <= 0 {
		return nil, errors.New("handlers: Users, Auth, DB, Limiter, RateLimit and Logger are required")
	}
	for _, policy := range []struct {
		limit  *int
		window *time.Duration
	}{
		{&deps.RateLimit.RegisterLimit, &deps.RateLimit.RegisterWindow},
		{&deps.RateLimit.RecoveryLimit, &deps.RateLimit.RecoveryWindow},
		{&deps.RateLimit.SessionLimit, &deps.RateLimit.SessionWindow},
	} {
		if *policy.limit == 0 {
			*policy.limit = deps.RateLimit.Limit
		}
		if *policy.window == 0 {
			*policy.window = deps.RateLimit.Window
		}
		if *policy.limit < 0 || *policy.window < 0 {
			return nil, errors.New("handlers: rate-limit policies must be positive")
		}
	}
	return &Handler{deps: deps}, nil
}
