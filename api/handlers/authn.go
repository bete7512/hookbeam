package handlers

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

type access int

const (
	accessRequired access = iota // zero value, so an unknown route requires a login
	accessAnonymous
	accessOptional
)

// Security scheme names as the spec declares them.
const (
	schemeBearer = "bearerAuth"
	schemeCookie = "cookieAuth"
	schemeAPIKey = "apiKeyAuth"
)

// accessRule is what one operation accepts: whether a credential is needed and which kinds.
type accessRule struct {
	mode   access
	user   bool     // bearerAuth or cookieAuth: a goauth session
	apiKey bool     // apiKeyAuth: a workspace API key
	scopes []string // scopes the apiKeyAuth requirement lists; a key must hold every one
}

// accessModes reads each operation's security requirement from the spec, keyed like r.Pattern.
// Only an explicit `security: []` makes an operation public. A protected operation that names
// no known scheme accepts a user session, so a typo still fails closed rather than open.
func accessModes(doc *openapi3.T) map[string]accessRule {
	modes := map[string]accessRule{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			requirements := doc.Security
			if op.Security != nil {
				requirements = *op.Security
			}
			rule := accessRule{mode: accessRequired}
			switch {
			case op.Security == nil && doc.Security == nil:
				// Nothing declared anywhere: fail closed.
			case len(requirements) == 0:
				rule.mode = accessAnonymous
			case slices.ContainsFunc(requirements, func(r openapi3.SecurityRequirement) bool { return len(r) == 0 }):
				rule.mode = accessOptional
			}
			for _, req := range requirements {
				for scheme, scopes := range req {
					switch scheme {
					case schemeBearer, schemeCookie:
						rule.user = true
					case schemeAPIKey:
						rule.apiKey = true
						if len(scopes) > 0 {
							rule.scopes = scopes
						}
					}
				}
			}
			if rule.mode != accessAnonymous && !rule.user && !rule.apiKey {
				rule.user = true
			}
			modes[method+" "+path] = rule
		}
	}
	return modes
}

// authenticate enforces each route's security requirement and stores the caller's principal in the context.
func (h *Handler) authenticate(modes map[string]accessRule) openapi.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule := modes[r.Pattern]
			if rule.mode == accessAnonymous {
				next.ServeHTTP(w, r)
				return
			}

			token := h.accessToken(r)
			if token == "" {
				if rule.mode == accessOptional {
					next.ServeHTTP(w, r)
					return
				}
				httpx.WriteError(w, r, apperr.NewUnauthorized("authentication required", nil))
				return
			}

			var (
				principal types.Principal
				key       *models.APIKey
				err       error
			)
			switch {
			case strings.HasPrefix(token, models.APIKeyPrefix):
				if !rule.apiKey {
					httpx.WriteError(w, r, apperr.NewUnauthorized("this route does not accept api keys", nil))
					return
				}
				principal, key, err = h.deps.APIKeys.Authenticate(r.Context(), token)
				for _, scope := range rule.scopes {
					if err == nil && !slices.Contains(key.Scopes, scope) {
						err = apperr.NewForbidden("api key lacks scope "+scope, nil)
					}
				}
			default:
				if !rule.user {
					httpx.WriteError(w, r, apperr.NewUnauthorized("this route accepts api keys only", nil))
					return
				}
				principal, err = h.deps.Auth.Authenticate(r.Context(), token)
				if err != nil {
					err = apperr.NewUnauthorized("invalid or expired token", err)
				}
			}
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			ctx := types.WithPrincipal(r.Context(), principal)
			if key != nil {
				ctx = context.WithValue(ctx, apiKeyContextKey{}, key)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type apiKeyContextKey struct{}

// apiKeyFrom returns the API key the request authenticated with, if any.
func apiKeyFrom(r *http.Request) (*models.APIKey, bool) {
	key, ok := r.Context().Value(apiKeyContextKey{}).(*models.APIKey)
	return key, ok && key != nil
}

// accessToken reads the token from the Authorization header or the access cookie. A request that carries both
// with different values names two identities and gets neither.
func (h *Handler) accessToken(r *http.Request) string {
	var bearer, cookie string
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		bearer = strings.TrimSpace(token)
	}
	if c, err := r.Cookie(h.deps.AccessCookie); err == nil {
		cookie = c.Value
	}
	switch {
	case bearer != "" && cookie != "" && bearer != cookie:
		return ""
	case bearer != "":
		return bearer
	default:
		return cookie
	}
}

// principal returns the authenticated caller or an unauthorized error.
func principal(r *http.Request) (types.Principal, error) {
	p, ok := types.PrincipalFromContext(r.Context())
	if !ok {
		return types.Principal{}, apperr.NewUnauthorized("authentication required", nil)
	}
	return p, nil
}

// actor returns the caller acting in a workspace. A session carries its user; an API key, admitted only on the
// operations the spec marks apiKeyAuth, carries the workspace alone and the services treat it as such.
func actor(r *http.Request) (services.Actor, error) {
	p, err := principal(r)
	if err != nil {
		return services.Actor{}, err
	}
	if p.OrganizationID == "" {
		return services.Actor{}, apperr.NewForbidden("select a workspace", nil)
	}
	return services.Actor{UserID: p.UserID, WorkspaceID: p.OrganizationID}, nil
}
