// Package handlers implements the generated openapi.ServerInterface.
// Handlers decode requests into generated DTOs, call services, and write
// responses; every error goes through httpx.WriteError.
package handlers

import (
	"context"
	"log/slog"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Authenticator validates an access token; *goauth.Auth satisfies it.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (types.Principal, error)
}

// Deps are the handler dependencies; all are required.
type Deps struct {
	Auth           Authenticator
	APIKeys        services.APIKeyService
	DB             Pinger
	Delivery       services.DeliveryService // the data plane, over gRPC
	DeliveryHealth Pinger                   // its health, reported by /readyz
	Members        services.MemberService
	OrgPolicy      services.OrgPolicy // guards goauth's invite, role-change and remove routes
	Audit          services.AuditService
	Notifications  services.NotificationService
	Authz          services.Authorizer // the caller's role for /v1/me
	Limiter        ratelimit.Limiter   // counts submissions per API key
	KeySubmitLimit int                 // submissions a key may make per minute
	AccessCookie   string              // cookie that carries the access token for browser clients
	Logger         *slog.Logger
}

// Handler serves every operation in the OpenAPI spec.
type Handler struct {
	deps Deps
}

var _ openapi.ServerInterface = (*Handler)(nil)

// New returns a Handler or an error if a dependency is missing.
func New(deps Deps) (*Handler, error) {
	if err := depcheck.Required("handlers", deps); err != nil {
		return nil, err
	}
	return &Handler{deps: deps}, nil
}
