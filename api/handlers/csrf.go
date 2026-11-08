package handlers

import (
	"net/http"
	"net/url"

	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/httpx"
)

// crossSiteGuard refuses state-changing requests that ride on the session cookie from another origin. A browser
// sends Origin on every cross-site request and on same-origin POSTs; a request that carries the cookie, uses an
// unsafe method and names no allowed origin is a forgery or a client that should use a bearer token. Bearer
// requests and cookie-less requests are not the browser's ambient authority and pass.
func crossSiteGuard(cookie string, allowed []string) httpx.Middleware {
	origins := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		if u, err := url.Parse(a); err == nil && u.Scheme != "" && u.Host != "" {
			origins[u.Scheme+"://"+u.Host] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("Authorization") != "" {
				next.ServeHTTP(w, r)
				return
			}
			if _, err := r.Cookie(cookie); err != nil {
				next.ServeHTTP(w, r)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				if ref, err := url.Parse(r.Header.Get("Referer")); err == nil && ref.Scheme != "" && ref.Host != "" {
					origin = ref.Scheme + "://" + ref.Host
				}
			}
			if origin == "" || !origins[origin] {
				httpx.WriteError(w, r, apperr.NewForbidden("cross-site request refused: the session cookie is only accepted from the console's origin", nil))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
