package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/oidc"
)

const (
	clientID     = "client-123"
	clientSecret = "secret-456"
	redirectURL  = "http://localhost:8080/v1/auth/google/callback"
	code         = "code-789"
)

// fakeIssuer is a minimal OpenID provider: discovery, JWKS and a token endpoint.
type fakeIssuer struct {
	srv        *httptest.Server
	key        *rsa.PrivateKey
	challenge  string        // code_challenge recorded from the auth URL
	idToken    func() string // id_token returned by /token; nil returns none
	blockToken bool
	blockKeys  bool
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("GET /jwks", f.jwks)
	mux.HandleFunc("POST /token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                f.srv.URL,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/jwks",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIssuer) jwks(w http.ResponseWriter, r *http.Request) {
	if f.blockKeys {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
		return
	}
	pub := f.key.Public().(*rsa.PublicKey)
	writeJSON(w, map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": "test",
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (f *fakeIssuer) token(w http.ResponseWriter, r *http.Request) {
	if f.blockToken {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	verifier := r.PostForm.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	switch {
	case id != clientID || secret != clientSecret:
		oauthError(w, "invalid_client")
	case r.PostForm.Get("grant_type") != "authorization_code",
		r.PostForm.Get("code") != code,
		r.PostForm.Get("redirect_uri") != redirectURL,
		f.challenge == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge:
		oauthError(w, "invalid_grant")
	default:
		resp := map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600}
		if f.idToken != nil {
			resp["id_token"] = f.idToken()
		}
		writeJSON(w, resp)
	}
}

// sign returns an RS256 id_token for this issuer; overrides patch the default claims.
func (f *fakeIssuer) sign(key *rsa.PrivateKey, overrides map[string]any) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":            f.srv.URL,
		"aud":            clientID,
		"sub":            "google-uid-1",
		"iat":            now.Unix(),
		"exp":            now.Add(time.Hour).Unix(),
		"email":          "ada@example.com",
		"email_verified": true,
		"name":           "Ada Lovelace",
	}
	for k, v := range overrides {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test"
	s, err := tok.SignedString(key)
	if err != nil {
		panic(err)
	}
	return s
}

func (f *fakeIssuer) provider(t *testing.T) oidc.Provider {
	t.Helper()
	p, err := oidc.New(context.Background(), oidc.Config{
		Issuer:       f.srv.URL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
	})
	require.NoError(t, err)
	return p
}

// start runs the auth-URL step and records the challenge the token endpoint must see.
func (f *fakeIssuer) start(t *testing.T, p oidc.Provider, verifier string) {
	t.Helper()
	u, err := url.Parse(p.AuthCodeURL("state", verifier))
	require.NoError(t, err)
	f.challenge = u.Query().Get("code_challenge")
	require.NotEmpty(t, f.challenge)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func TestNew(t *testing.T) {
	f := newFakeIssuer(t)
	tests := []struct {
		name    string
		cfg     oidc.Config
		wantErr bool
	}{
		{name: "discovers", cfg: oidc.Config{Issuer: f.srv.URL, ClientID: clientID, RedirectURL: redirectURL}},
		{name: "missing client id", cfg: oidc.Config{Issuer: f.srv.URL, RedirectURL: redirectURL}, wantErr: true},
		{name: "unreachable issuer", cfg: oidc.Config{Issuer: "http://127.0.0.1:1", ClientID: clientID, RedirectURL: redirectURL}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := oidc.New(context.Background(), tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, p)
		})
	}
}

func TestAuthCodeURL(t *testing.T) {
	f := newFakeIssuer(t)
	verifier, err := oidc.NewVerifier()
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(verifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	tests := []struct {
		name   string
		scopes []string
		want   map[string]string
	}{
		{name: "default scopes", want: map[string]string{
			"client_id":             clientID,
			"redirect_uri":          redirectURL,
			"response_type":         "code",
			"state":                 "state-1",
			"scope":                 "openid email profile",
			"code_challenge_method": "S256",
			"code_challenge":        wantChallenge,
		}},
		{name: "custom scopes", scopes: []string{"openid", "email"}, want: map[string]string{
			"scope":          "openid email",
			"code_challenge": wantChallenge,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := oidc.New(context.Background(), oidc.Config{
				Issuer: f.srv.URL, ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL, Scopes: tt.scopes,
			})
			require.NoError(t, err)
			u, err := url.Parse(p.AuthCodeURL("state-1", verifier))
			require.NoError(t, err)
			require.Equal(t, f.srv.URL+"/authorize", u.Scheme+"://"+u.Host+u.Path)
			q := u.Query()
			for k, v := range tt.want {
				require.Equal(t, v, q.Get(k), k)
			}
		})
	}
}

func TestExchange(t *testing.T) {
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tests := []struct {
		name          string
		idToken       func(f *fakeIssuer) string // nil: no id_token in the response
		wrongVerifier bool
		want          oidc.Identity
		wantErr       error
		wantAnyErr    bool
	}{
		{
			name:    "happy path",
			idToken: func(f *fakeIssuer) string { return f.sign(f.key, nil) },
			want:    oidc.Identity{Subject: "google-uid-1", Email: "ada@example.com", EmailVerified: true, Name: "Ada Lovelace"},
		},
		{
			name:    "signed hosted domain is retained",
			idToken: func(f *fakeIssuer) string { return f.sign(f.key, map[string]any{"hd": "example.com"}) },
			want:    oidc.Identity{Subject: "google-uid-1", Email: "ada@example.com", EmailVerified: true, HostedDomain: "example.com", Name: "Ada Lovelace"},
		},
		{
			name:          "wrong verifier",
			idToken:       func(f *fakeIssuer) string { return f.sign(f.key, nil) },
			wrongVerifier: true,
			wantAnyErr:    true,
		},
		{
			name:    "wrong audience",
			idToken: func(f *fakeIssuer) string { return f.sign(f.key, map[string]any{"aud": "someone-else"}) },
			wantErr: oidc.ErrInvalidToken,
		},
		{
			name:    "signed by another key",
			idToken: func(f *fakeIssuer) string { return f.sign(otherKey, nil) },
			wantErr: oidc.ErrInvalidToken,
		},
		{
			name: "expired",
			idToken: func(f *fakeIssuer) string {
				return f.sign(f.key, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})
			},
			wantErr: oidc.ErrInvalidToken,
		},
		{
			name:    "wrong issuer",
			idToken: func(f *fakeIssuer) string { return f.sign(f.key, map[string]any{"iss": "https://evil.example"}) },
			wantErr: oidc.ErrInvalidToken,
		},
		{
			name:    "no id_token",
			wantErr: oidc.ErrInvalidToken,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIssuer(t)
			if tt.idToken != nil {
				f.idToken = func() string { return tt.idToken(f) }
			}
			p := f.provider(t)
			verifier, err := oidc.NewVerifier()
			require.NoError(t, err)
			f.start(t, p, verifier)
			if tt.wrongVerifier {
				verifier, err = oidc.NewVerifier()
				require.NoError(t, err)
			}

			got, err := p.Exchange(context.Background(), code, verifier)
			if tt.wantAnyErr {
				require.Error(t, err)
				require.NotErrorIs(t, err, oidc.ErrInvalidToken)
				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRandomValues(t *testing.T) {
	unreserved := regexp.MustCompile(`^[A-Za-z0-9\-._~]+$`)
	tests := []struct {
		name string
		gen  func() (string, error)
	}{
		{name: "NewState", gen: oidc.NewState},
		{name: "NewVerifier", gen: oidc.NewVerifier},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := tt.gen()
			require.NoError(t, err)
			b, err := tt.gen()
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(a), 43)
			require.LessOrEqual(t, len(a), 128)
			require.Regexp(t, unreserved, a)
			require.NotEqual(t, a, b)
		})
	}
}

func TestExchangeTimeout(t *testing.T) {
	for _, endpoint := range []string{"token", "keys"} {
		t.Run(endpoint, func(t *testing.T) {
			f := newFakeIssuer(t)
			f.blockToken = endpoint == "token"
			f.blockKeys = endpoint == "keys"
			f.idToken = func() string { return f.sign(f.key, nil) }
			p, err := oidc.New(context.Background(), oidc.Config{
				Issuer: f.srv.URL, ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL, HTTPTimeout: 100 * time.Millisecond,
			})
			require.NoError(t, err)
			verifier, err := oidc.NewVerifier()
			require.NoError(t, err)
			f.start(t, p, verifier)
			_, err = p.Exchange(context.Background(), code, verifier)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}
