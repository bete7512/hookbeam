package httpx

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"
)

// NewInternalServer serves the operator endpoints on a listener that is never public: /healthz, and the Go
// profiler under /debug/pprof/ behind a bearer token.
func NewInternalServer(addr, token string, log *slog.Logger) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="internal"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/debug/pprof/", guard(pprof.Index))
	mux.HandleFunc("/debug/pprof/cmdline", guard(pprof.Cmdline))
	mux.HandleFunc("/debug/pprof/profile", guard(pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", guard(pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", guard(pprof.Trace))
	return NewServer(mux, Options{Addr: addr, ReadTimeout: 5 * time.Minute, WriteTimeout: 5 * time.Minute, Logger: log})
}
