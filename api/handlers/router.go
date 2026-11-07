package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// RouterOptions configures NewRouter. AuthRoutes are goauth's routes, mounted as
// they are; AuthSpec is goauth's OpenAPI document, merged into ours under AuthBasePath.
type RouterOptions struct {
	Logger       *slog.Logger
	MaxBodyBytes int64
	AuthRoutes   []goauthcfg.RouteInfo
	AuthSpec     []byte
	AuthBasePath string
}

// NewRouter returns the service's http.Handler: every route from the OpenAPI
// spec, goauth's routes, GET /openapi.json, GET /docs and a JSON 404, wrapped in
// the standard middleware chain.
func NewRouter(h *Handler, o RouterOptions) (http.Handler, error) {
	doc, err := openapi.GetSpec()
	if err != nil {
		return nil, fmt.Errorf("handlers: load embedded OpenAPI spec: %w", err)
	}
	modes := accessModes(doc)
	if len(o.AuthSpec) > 0 {
		if err := mergeAuthSpec(doc, o.AuthSpec, o.AuthBasePath); err != nil {
			return nil, err
		}
	}
	spec, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("handlers: encode OpenAPI spec: %w", err)
	}

	mux := http.NewServeMux()
	openapi.HandlerWithOptions(h, openapi.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []openapi.MiddlewareFunc{h.authenticate(modes)},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			httpx.WriteError(w, r, apperr.NewBadRequest("invalid path or query parameter", err))
		},
	})
	for _, route := range o.AuthRoutes {
		mux.Handle(route.Method+" "+route.Path, h.guardOrgRoute(route))
	}
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", httpx.ContentTypeJSON)
		_, _ = w.Write(spec)
	})
	mux.HandleFunc("GET /docs", httpx.Docs("/openapi.json"))
	mux.HandleFunc("/", httpx.NotFound())

	return httpx.Chain(mux, httpx.Default(o.Logger, o.MaxBodyBytes)...), nil
}

// mergeAuthSpec adds goauth's paths and components to doc, prefixing paths with basePath.
// A component name used by both documents is a startup error, never a silent override.
func mergeAuthSpec(doc *openapi3.T, authSpec []byte, basePath string) error {
	authDoc, err := openapi3.NewLoader().LoadFromData(authSpec)
	if err != nil {
		return fmt.Errorf("handlers: load goauth OpenAPI spec: %w", err)
	}
	for path, item := range authDoc.Paths.Map() {
		full := basePath + path
		if doc.Paths.Value(full) != nil {
			return fmt.Errorf("handlers: route %s is defined by both specs", full)
		}
		doc.Paths.Set(full, item)
	}
	if authDoc.Components == nil {
		return nil
	}
	if doc.Components == nil {
		doc.Components = &openapi3.Components{}
	}
	if err := mergeComponents(&doc.Components.Schemas, authDoc.Components.Schemas, "schema"); err != nil {
		return err
	}
	if err := mergeComponents(&doc.Components.Responses, authDoc.Components.Responses, "response"); err != nil {
		return err
	}
	if err := mergeComponents(&doc.Components.Parameters, authDoc.Components.Parameters, "parameter"); err != nil {
		return err
	}
	if err := mergeComponents(&doc.Components.RequestBodies, authDoc.Components.RequestBodies, "request body"); err != nil {
		return err
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = openapi3.SecuritySchemes{}
	}
	for name, scheme := range authDoc.Components.SecuritySchemes {
		if _, ok := doc.Components.SecuritySchemes[name]; !ok {
			doc.Components.SecuritySchemes[name] = scheme
		}
	}
	return nil
}

func mergeComponents[M ~map[string]V, V any](dst *M, src M, kind string) error {
	if len(src) == 0 {
		return nil
	}
	if *dst == nil {
		*dst = M{}
	}
	for name, v := range src {
		if _, ok := (*dst)[name]; ok {
			return fmt.Errorf("handlers: OpenAPI %s %q is defined by both specs", kind, name)
		}
		(*dst)[name] = v
	}
	return nil
}
