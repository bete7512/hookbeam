package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns a canonical IP, falling back to the connection for invalid forwarded values.
// trustProxy requires one trusted append-mode proxy and blocked direct ingress; it does not validate a proxy chain.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		values := r.Header.Values("X-Forwarded-For")
		if len(values) > 0 {
			last := values[len(values)-1]
			if i := strings.LastIndexByte(last, ','); i >= 0 {
				last = last[i+1:]
			}
			if ip, ok := canonicalIP(strings.TrimSpace(last)); ok {
				return ip
			}
		}
	}
	if ip, ok := canonicalIP(r.RemoteAddr); ok {
		return ip
	}
	return "unknown"
}

func canonicalIP(value string) (string, bool) {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		address, parseErr := netip.ParseAddrPort(value)
		if parseErr != nil {
			return "", false
		}
		ip = address.Addr()
	}
	return ip.WithZone("").Unmap().String(), true
}
