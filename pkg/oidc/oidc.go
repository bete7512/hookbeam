// Package oidc logs users in with an OpenID Connect provider using the authorization code flow with PKCE.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ErrInvalidToken is wrapped by every Exchange failure that means the provider's answer is not trustworthy.
var ErrInvalidToken = errors.New("oidc: invalid token")

var defaultScopes = []string{gooidc.ScopeOpenID, "email", "profile"}

// Config identifies the provider and this client.
type Config struct {
	Issuer       string // e.g. https://accounts.google.com; discovery is read from <issuer>/.well-known/openid-configuration
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HTTPTimeout  time.Duration // defaults to ten seconds
	Scopes       []string      // defaults to openid, email, profile when empty
}

// Identity is what the provider asserts about the user after a successful login.
type Identity struct {
	Subject       string // stable per provider; store this, never the email, as the identity key
	Email         string
	EmailVerified bool
	HostedDomain  string // signed Google hd claim; empty for consumer accounts
	Name          string
}

// Provider starts and completes logins; the caller keeps state and verifier between the two calls.
type Provider interface {
	AuthCodeURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (Identity, error)
}

type provider struct {
	oauth    oauth2.Config
	verifier *gooidc.IDTokenVerifier
	client   *http.Client
	timeout  time.Duration
}

// New discovers the issuer and returns a Provider.
func New(ctx context.Context, cfg Config) (Provider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, errors.New("oidc: issuer, client id and redirect url are required")
	}
	timeout := cfg.HTTPTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	if timeout < 0 {
		return nil, errors.New("oidc: HTTP timeout must be positive")
	}
	client := &http.Client{Timeout: timeout}
	ctx = gooidc.ClientContext(ctx, client)
	p, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover %s: %w", cfg.Issuer, err)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = defaultScopes
	}
	return &provider{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       scopes,
		},
		verifier: p.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		client:   client, timeout: timeout,
	}, nil
}

func (p *provider) AuthCodeURL(state, verifier string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

type idClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	HostedDomain  string `json:"hd"`
	Name          string `json:"name"`
}

func (p *provider) Exchange(ctx context.Context, code, verifier string) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	ctx = gooidc.ClientContext(ctx, p.client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: exchange: %w", err)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return Identity{}, fmt.Errorf("%w: token response has no id_token", ErrInvalidToken)
	}
	idToken, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		if ctx.Err() != nil {
			return Identity{}, fmt.Errorf("oidc: verify: %w", ctx.Err())
		}
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	return Identity{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		HostedDomain:  claims.HostedDomain,
		Name:          claims.Name,
	}, nil
}

// NewState returns a random URL-safe state value.
func NewState() (string, error) {
	return randomString(32)
}

// NewVerifier returns a random PKCE code verifier (43–128 chars, unreserved characters).
func NewVerifier() (string, error) {
	return randomString(32)
}

// randomString returns n random bytes encoded as unpadded base64url (43 chars for n=32).
func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oidc: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
