//go:build security

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

// fakeResolver answers every lookup with the same addresses, standing in for a DNS that rebinds a public name
// to a private address between validation and connection.
type fakeResolver struct{ ips []net.IPAddr }

func (f fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) { return f.ips, nil }

// TestSecuritySSRFDestinations covers the spec's outbound rules: nonpublic addresses in every form are refused,
// credentials in URLs are rejected and only allowed ports and https pass.
func TestSecuritySSRFDestinations(t *testing.T) {
	cfg := safehttp.Config{Timeout: 2 * time.Second, AllowedPorts: []int{443, 8443}}
	for _, u := range []string{
		"https://127.0.0.1/hooks", "https://10.1.2.3/hooks", "https://192.168.1.9/hooks", "https://172.16.0.1/hooks",
		"https://169.254.169.254/latest/meta-data", "https://[::1]/hooks", "https://[fd00::1]/hooks", "https://[::ffff:127.0.0.1]/hooks",
		"https://[::ffff:10.0.0.1]/hooks", "https://100.64.0.1/hooks", "https://0.0.0.0/hooks", "https://224.0.0.1/hooks",
	} {
		t.Run("blocked "+u, func(t *testing.T) {
			require.ErrorIs(t, cfg.ValidateURL(u), safehttp.ErrBlockedDestination)
		})
	}
	for name, u := range map[string]string{
		"http scheme":      "http://receiver.example.com/hooks",
		"credentials":      "https://user:pw@receiver.example.com/hooks",
		"port not allowed": "https://receiver.example.com:8080/hooks",
		"no host":          "https:///hooks",
		"bad scheme":       "ftp://receiver.example.com/hooks",
	} {
		t.Run("rejected "+name, func(t *testing.T) {
			require.Error(t, cfg.ValidateURL(u))
		})
	}
	require.NoError(t, cfg.ValidateURL("https://receiver.example.com/hooks"))
	require.NoError(t, cfg.ValidateURL("https://receiver.example.com:8443/hooks"))
}

// TestSecuritySSRFRebinding resolves a public-looking name to a loopback address at dial time, as a rebinding
// attack does after validation; the dialer validates what it resolves and refuses.
func TestSecuritySSRFRebinding(t *testing.T) {
	cfg := safehttp.Config{Timeout: 2 * time.Second, AllowedPorts: []int{443}, Resolver: fakeResolver{ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}}
	_, err := cfg.NewClient().Get("https://public.example.com/hooks")
	require.ErrorIs(t, err, safehttp.ErrBlockedDestination)

	mapped := safehttp.Config{Timeout: 2 * time.Second, AllowedPorts: []int{443}, Resolver: fakeResolver{ips: []net.IPAddr{{IP: net.ParseIP("::ffff:169.254.169.254")}}}}
	_, err = mapped.NewClient().Get("https://public.example.com/hooks")
	require.ErrorIs(t, err, safehttp.ErrBlockedDestination, "the metadata address in IPv4-mapped IPv6 form")
}

// TestSecuritySSRFRedirects proves a 302 is recorded, not followed, even toward an allowed destination.
func TestSecuritySSRFRedirects(t *testing.T) {
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { followed = true; w.WriteHeader(http.StatusOK) }))
	defer target.Close()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirecting.Close()
	client := safehttp.Config{Timeout: 2 * time.Second, AllowPrivate: true}.NewClient()
	resp, err := client.Get(redirecting.URL)
	if err != nil {
		require.ErrorIs(t, err, safehttp.ErrRedirect)
	} else {
		resp.Body.Close()
		require.Equal(t, http.StatusFound, resp.StatusCode)
	}
	require.False(t, followed, "the redirect target was never contacted")
}
