// Package handlers implements the generated openapi.ServerInterface.
// Handlers decode requests into generated DTOs, call services, and write
// responses; every error goes through httpx.WriteError.
package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/gen/openapi"
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
	Auth         Authenticator
	DB           Pinger
	AccessCookie string // cookie that carries the access token for browser clients
	Logger       *slog.Logger
}

// Handler serves every operation in the OpenAPI spec.
type Handler struct {
	deps Deps
}

var _ openapi.ServerInterface = (*Handler)(nil)

// New returns a Handler or an error if a dependency is missing.
func New(deps Deps) (*Handler, error) {
	if deps.Auth == nil || deps.DB == nil || deps.AccessCookie == "" || deps.Logger == nil {
		return nil, errors.New("handlers: Auth, DB, AccessCookie and Logger are required")
	}
	return &Handler{deps: deps}, nil
}
