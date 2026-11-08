// Package safehttp builds HTTP clients for calling customer-supplied URLs: bounded, redirect-free and
// unable to reach private, loopback, link-local or cloud-metadata addresses.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// Errors a send can fail with; callers classify on them.
var (
	ErrScheme             = errors.New("safehttp: scheme not allowed")
	ErrPort               = errors.New("safehttp: port not allowed")
	ErrBlockedDestination = errors.New("safehttp: destination address is not allowed")
	ErrRedirect           = errors.New("safehttp: redirects are not followed")
	ErrURL                = errors.New("safehttp: invalid url")
)

// Config bounds the client.
type Config struct {
	// Timeout covers DNS, connect, TLS and response headers of one request.
	Timeout time.Duration
	// AllowedPorts is the destination port allowlist; nil allows the scheme's default only.
	AllowedPorts []int
	// AllowPrivate permits http and private, loopback and link-local destinations: local development only.
	AllowPrivate bool
	// Resolver answers the lookups the dialer validates; nil means the system resolver. Tests inject one.
	Resolver Resolver
}

// Resolver is the part of net.Resolver the dialer uses.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ValidateURL checks scheme, host, credentials and port against cfg without resolving the host.
func (c Config) ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return ErrURL
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in url", ErrURL)
	}
	// A literal address is judged now; a hostname is judged at dial time, after it resolves.
	if ip := net.ParseIP(u.Hostname()); ip != nil && !c.AllowPrivate && Blocked(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedDestination, u.Hostname())
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !c.AllowPrivate {
			return fmt.Errorf("%w: %s", ErrScheme, u.Scheme)
		}
	default:
		return fmt.Errorf("%w: %s", ErrScheme, u.Scheme)
	}
	port, err := portOf(u)
	if err != nil {
		return err
	}
	if c.AllowPrivate || len(c.AllowedPorts) == 0 && (port == 443 || port == 80) || slices.Contains(c.AllowedPorts, port) {
		return nil
	}
	return fmt.Errorf("%w: %d", ErrPort, port)
}

// NewClient returns a client that validates every destination at dial time, after DNS resolution,
// so a hostname that resolves to a private address is refused too.
func (c Config) NewClient() *http.Client {
	dialer := &net.Dialer{Timeout: c.Timeout}
	transport := &http.Transport{
		Proxy:                 nil, // never through an environment proxy
		DialContext:           c.dial(dialer),
		TLSHandshakeTimeout:   c.Timeout,
		ResponseHeaderTimeout: c.Timeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   c.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return ErrRedirect
		},
	}
}

func (c Config) dial(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var resolver Resolver = net.DefaultResolver
		if c.Resolver != nil {
			resolver = c.Resolver
		}
		ips, err := resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		lastErr := ErrBlockedDestination
		for _, ip := range ips {
			if !c.AllowPrivate && Blocked(ip.IP) {
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// Blocked reports whether ip is private, loopback, link-local, multicast, unspecified or a cloud metadata address,
// including IPv4-mapped IPv6 forms.
func Blocked(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() ||
		ip.Equal(net.IPv4(169, 254, 169, 254)) || ip.Equal(net.ParseIP("fd00:ec2::254")) ||
		len(ip) == net.IPv4len && ip[0] == 100 && ip[1]&0xc0 == 64 // 100.64.0.0/10 carrier-grade NAT
}

func portOf(u *url.URL) (int, error) {
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port <= 0 || port > 65535 {
			return 0, fmt.Errorf("%w: port %q", ErrURL, p)
		}
		return port, nil
	}
	if u.Scheme == "http" {
		return 80, nil
	}
	return 443, nil
}
