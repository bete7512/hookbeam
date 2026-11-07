package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bete7512/scaffold/apps/api/gen/openapi"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/httpx"
)

// RouterOptions configures NewRouter.
type RouterOptions struct {
	Logger       *slog.Logger
	MaxBodyBytes int64
}

// NewRouter returns the service's http.Handler: every route from the OpenAPI
// spec, GET /openapi.json, GET /docs and a JSON 404, wrapped in the standard
// middleware chain.
func NewRouter(h *Handler, o RouterOptions) (http.Handler, error) {
	spec, err := openapi.GetSpecJSON()
	if err != nil {
		return nil, fmt.Errorf("handlers: load embedded OpenAPI spec: %w", err)
	}

	doc, err := openapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("handlers: load embedded OpenAPI spec: %w", err)
	}

	mux := http.NewServeMux()
	openapi.HandlerWithOptions(h, openapi.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []openapi.MiddlewareFunc{h.authenticate(accessModes(doc)), h.rateLimit()},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.WriteError(w, r, apperr.NewBadRequest("invalid path or query parameter", err))
		},
	})
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", httpx.ContentTypeJSON)
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("GET /docs", httpx.Docs("/openapi.json"))
	mux.HandleFunc("/", httpx.NotFound())

	return httpx.Chain(mux, httpx.Default(o.Logger, o.MaxBodyBytes)...), nil
}
