package handlers

import (
	"net/http"
	"slices"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/auth"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

type access int

const (
	accessRequired access = iota // zero value, so an unknown route requires a login
	accessAnonymous
	accessOptional
)

// accessModes reads each operation's security requirement from the spec, keyed like r.Pattern.
// Only an explicit `security: []` makes an operation public.
func accessModes(doc *openapi3.T) map[string]access {
	modes := map[string]access{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			requirements := doc.Security
			if op.Security != nil {
				requirements = *op.Security
			}
			mode := accessRequired
			switch {
			case op.Security == nil && doc.Security == nil:
				// Nothing declared anywhere: fail closed.
			case len(requirements) == 0:
				mode = accessAnonymous
			case slices.ContainsFunc(requirements, func(r openapi3.SecurityRequirement) bool { return len(r) == 0 }):
				mode = accessOptional
			}
			modes[method+" "+path] = mode
		}
	}
	return modes
}

// authenticate enforces each route's security requirement and stores the caller's claims in the context.
func (h *Handler) authenticate(modes map[string]access) openapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mode := modes[r.Pattern]
			if mode == accessAnonymous {
				next.ServeHTTP(w, r)
				return
			}

			token := accessToken(r)
			if token == "" {
				if mode == accessOptional {
					next.ServeHTTP(w, r)
					return
				}
				httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
				return
			}

			claims, err := h.deps.Auth.Authenticate(r.Context(), token)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
}

// accessToken reads the token from the Authorization header or the access_token cookie.
func accessToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	if cookie, err := r.Cookie("access_token"); err == nil {
		return cookie.Value
	}
	return ""
}
