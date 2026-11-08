package httpx_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/httpx"
)

func TestInternalServerGuardsProfiler(t *testing.T) {
	srv := httpx.NewInternalServer(":0", "s3cret", slog.New(slog.DiscardHandler))
	h := srv.Handler()
	for _, tt := range []struct {
		name   string
		path   string
		token  string
		status int
	}{
		{"health is open", "/healthz", "", http.StatusNoContent},
		{"pprof without a token", "/debug/pprof/", "", http.StatusUnauthorized},
		{"pprof with a wrong token", "/debug/pprof/", "nope", http.StatusUnauthorized},
		{"pprof with the token", "/debug/pprof/", "s3cret", http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.token != "" {
				req.Header.Set("Authorization", "Bearer "+tt.token)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
		})
	}
	empty := httpx.NewInternalServer(":0", "", slog.New(slog.DiscardHandler)).Handler()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	empty.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "an empty token never opens the profiler")
}
