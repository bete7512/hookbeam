package config_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/config"
)

const (
	dbURL         = "postgres://app:secret@localhost:5432/app"
	secretKey     = "secret-key-secret-key-secret-key-00"
	encryptionKey = "encryption-key-encryption-key-00"
)

func ecKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func valid(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{"DATABASE_URL": dbURL, "AUTH_SECRET_KEY": secretKey, "AUTH_ENCRYPTION_KEY": encryptionKey, "AUTH_JWT_PRIVATE_KEY": ecKey(t)}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		env      func(t *testing.T) map[string]string
		wantErrs []string
	}{
		{name: "defaults", env: valid},
		{
			name: "HS256 needs no key",
			env: func(t *testing.T) map[string]string {
				e := valid(t)
				delete(e, "AUTH_JWT_PRIVATE_KEY")
				e["AUTH_JWT_ALGORITHM"] = "HS256"
				return e
			},
		},
		{
			name:     "missing keys",
			env:      func(*testing.T) map[string]string { return map[string]string{"DATABASE_URL": dbURL} },
			wantErrs: []string{"AUTH_SECRET_KEY", "AUTH_ENCRYPTION_KEY", "AUTH_JWT_PRIVATE_KEY"},
		},
		{
			name: "same secret and encryption key",
			env: func(t *testing.T) map[string]string {
				e := valid(t)
				e["AUTH_ENCRYPTION_KEY"] = secretKey
				return e
			},
			wantErrs: []string{"must differ"},
		},
		{
			name: "not a PEM",
			env: func(t *testing.T) map[string]string {
				e := valid(t)
				e["AUTH_JWT_PRIVATE_KEY"] = base64.StdEncoding.EncodeToString([]byte("nope"))
				return e
			},
			wantErrs: []string{"not PEM"},
		},
		{
			name: "RS256 with an EC key",
			env: func(t *testing.T) map[string]string {
				e := valid(t)
				e["AUTH_JWT_ALGORITHM"] = "RS256"
				return e
			},
			wantErrs: []string{"RS256 needs an RSA key"},
		},
		{
			name: "shared and service errors together",
			env: func(t *testing.T) map[string]string {
				e := valid(t)
				e["APP_ENV"] = "qa"
				e["AUTH_LINK_BASE_URL"] = "console"
				return e
			},
			wantErrs: []string{"APP_ENV", "AUTH_LINK_BASE_URL"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse(tt.env(t))
			if len(tt.wantErrs) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErrs {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ":8080", cfg.HTTP.Addr)
			assert.Equal(t, "hookbeam", cfg.Auth.Issuer)
			if cfg.Auth.JWTAlgorithm != "HS256" {
				pemBytes, err := cfg.Auth.PrivateKeyPEM()
				require.NoError(t, err)
				assert.True(t, strings.HasPrefix(string(pemBytes), "-----BEGIN PRIVATE KEY-----"))
			}
		})
	}
}

func TestProductionConfig(t *testing.T) {
	prod := valid(t)
	for k, v := range map[string]string{"APP_ENV": "prod", "EVENTS_BACKEND": "aws", "EVENTS_TOPIC_ARN": "arn:aws:sns:us-east-1:123456789012:events", "EVENTS_QUEUE_URL": "https://sqs.us-east-1.amazonaws.com/123456789012/worker", "MAILER_BACKEND": "ses", "AUTH_LINK_BASE_URL": "https://console.example.com", "AUTH_API_URL": "https://api.example.com"} {
		prod[k] = v
	}
	_, err := config.Parse(prod)
	require.NoError(t, err)
	tests := []struct{ key, value string }{
		{"EVENTS_BACKEND", "memory"}, {"MAILER_BACKEND", "memory"}, {"MAILER_BACKEND", "smtp"},
		{"EVENTS_AWS_ENDPOINT_URL", "http://localhost:4566"},
		{"AUTH_LINK_BASE_URL", "http://console.example.com"}, {"AUTH_LINK_BASE_URL", "https://127.0.0.1"}, {"AUTH_LINK_BASE_URL", "https://localhost"},
		{"AUTH_API_URL", "http://api.example.com"},
		{"AUTH_ALLOW_INSECURE_COOKIES", "true"}, {"AUTH_JWT_ALGORITHM", "HS256"},
		{"EVENTS_HANDLER_TIMEOUT", "0s"}, {"OUTBOX_PUBLISH_TIMEOUT", "0s"}, {"OIDC_GOOGLE_CLIENT_ID", "id-without-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			changed := make(map[string]string, len(prod))
			for k, v := range prod {
				changed[k] = v
			}
			changed[tt.key] = tt.value
			_, err := config.Parse(changed)
			require.Error(t, err)
		})
	}
}
