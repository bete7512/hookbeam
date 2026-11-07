package httpx_test

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bete7512/hookbeam/pkg/httpx"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name      string
		remote    string
		forwarded []string
		trust     bool
		want      string
	}{
		{name: "direct IPv4", remote: "192.0.2.1:1234", want: "192.0.2.1"},
		{name: "untrusted header", remote: "192.0.2.1:1234", forwarded: []string{"198.51.100.2"}, want: "192.0.2.1"},
		{name: "rightmost address", remote: "192.0.2.1:1234", forwarded: []string{"198.51.100.2, 203.0.113.3"}, trust: true, want: "203.0.113.3"},
		{name: "last header line", remote: "192.0.2.1:1234", forwarded: []string{"spoofed", "203.0.113.3"}, trust: true, want: "203.0.113.3"},
		{name: "canonical IPv6", remote: "[2001:0db8:0:0:0:0:0:1]:1234", want: "2001:db8::1"},
		{name: "mapped IPv4", remote: "[::ffff:192.0.2.1]:1234", want: "192.0.2.1"},
		{name: "forwarded port", remote: "192.0.2.1:1234", forwarded: []string{"[2001:0db8::1]:443"}, trust: true, want: "2001:db8::1"},
		{name: "invalid forwarded falls back", remote: "192.0.2.1:1234", forwarded: []string{"203.0.113.3, arbitrary-key"}, trust: true, want: "192.0.2.1"},
		{name: "empty last forwarded falls back", remote: "192.0.2.1:1234", forwarded: []string{"203.0.113.3,"}, trust: true, want: "192.0.2.1"},
		{name: "bare remote IPv6", remote: "2001:0db8::1", want: "2001:db8::1"},
		{name: "invalid remote shares fallback", remote: "invalid", want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, value := range tt.forwarded {
				r.Header.Add("X-Forwarded-For", value)
			}
			assert.Equal(t, tt.want, httpx.ClientIP(r, tt.trust))
		})
	}
}
