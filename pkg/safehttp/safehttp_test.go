package safehttp_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/safehttp"
)

func TestValidateURL(t *testing.T) {
	strict := safehttp.Config{AllowedPorts: []int{443, 8443}}
	dev := safehttp.Config{AllowPrivate: true}
	tests := []struct {
		name    string
		cfg     safehttp.Config
		url     string
		wantErr error
	}{
		{name: "https default port", cfg: strict, url: "https://receiver.example.com/hooks"},
		{name: "https allowed port", cfg: strict, url: "https://receiver.example.com:8443/hooks"},
		{name: "https other port", cfg: strict, url: "https://receiver.example.com:9443/hooks", wantErr: safehttp.ErrPort},
		{name: "http in strict mode", cfg: strict, url: "http://receiver.example.com/hooks", wantErr: safehttp.ErrScheme},
		{name: "ftp", cfg: strict, url: "ftp://receiver.example.com/", wantErr: safehttp.ErrScheme},
		{name: "credentials", cfg: strict, url: "https://u:p@receiver.example.com/", wantErr: safehttp.ErrURL},
		{name: "no host", cfg: strict, url: "https:///hooks", wantErr: safehttp.ErrURL},
		{name: "bad port", cfg: strict, url: "https://receiver.example.com:99999/", wantErr: safehttp.ErrURL},
		{name: "http any port in dev", cfg: dev, url: "http://localhost:8081/hooks"},
		{name: "default allowlist is 443", cfg: safehttp.Config{}, url: "https://receiver.example.com/"},
		{name: "default allowlist refuses 8443", cfg: safehttp.Config{}, url: "https://receiver.example.com:8443/", wantErr: safehttp.ErrPort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateURL(tt.url)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBlocked(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "172.16.0.9", "192.168.1.1", "169.254.169.254", "169.254.1.1", "0.0.0.0", "224.0.0.1", "100.64.0.1", "::1", "fe80::1", "fd12::1", "::ffff:10.0.0.1", "::ffff:127.0.0.1", "fd00:ec2::254"} {
		require.True(t, safehttp.Blocked(net.ParseIP(ip)), ip)
	}
	for _, ip := range []string{"8.8.8.8", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946", "100.128.0.1"} {
		require.False(t, safehttp.Blocked(net.ParseIP(ip)), ip)
	}
}

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ctx := context.Background()

	strict := safehttp.Config{Timeout: 2 * time.Second}.NewClient()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	resp, err := strict.Do(req) //nolint:bodyclose // no response on a refused dial
	require.ErrorIs(t, err, safehttp.ErrBlockedDestination, "loopback is refused after resolution")
	require.Nil(t, resp)

	dev := safehttp.Config{Timeout: 2 * time.Second, AllowPrivate: true}.NewClient()
	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, nil)
	resp, err = dev.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	req, _ = http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/redirect", nil)
	resp, err = dev.Do(req)
	require.ErrorIs(t, err, safehttp.ErrRedirect)
	if resp != nil {
		resp.Body.Close()
	}
}
