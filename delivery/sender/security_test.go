//go:build security

package sender_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/sender"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/safehttp"
)

// TestSecurityResponseHeaderAllowlist: a receiver's cookies, authentication challenges and custom headers never
// reach the attempt record; only the allowlisted operational headers do.
func TestSecurityResponseHeaderAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "session=abc; HttpOnly")
		w.Header().Set("Authorization", "Bearer leaked")
		w.Header().Set("WWW-Authenticate", "Basic realm=x")
		w.Header().Set("X-Internal-Token", "t0ken")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ok":false}`))
	}))
	defer srv.Close()
	client := safehttp.Config{Timeout: 2 * time.Second, AllowPrivate: true}.NewClient()
	s := sender.New(client, sender.Config{MaxResponseBytes: 4096, MaxPreviewBytes: 64})
	res := s.Send(context.Background(), services.SendRequest{URL: srv.URL + "/hooks", Body: []byte(`{}`)})
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	for _, k := range []string{"set-cookie", "authorization", "www-authenticate", "x-internal-token"} {
		require.NotContains(t, res.Headers, k)
	}
	require.Equal(t, "application/json", res.Headers["content-type"])
	require.Equal(t, "7", res.Headers["retry-after"])
	for _, v := range res.Headers {
		require.NotContains(t, v, "leaked")
		require.NotContains(t, v, "t0ken")
	}
}
