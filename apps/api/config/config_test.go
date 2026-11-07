package config_test

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/apps/api/config"
)

const (
	dbURL      = "postgres://app:secret@localhost:5432/app"
	signingKey = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantErrs []string
	}{
		{name: "defaults", env: map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey}},
		{
			name:     "invalid auth settings",
			env:      map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey, "AUTH_ARGON2_MEMORY_KIB": "1024", "AUTH_ARGON2_ITERATIONS": "0"},
			wantErrs: []string{"AUTH_ARGON2_MEMORY_KIB", "AUTH_ARGON2_ITERATIONS"},
		},
		{
			name:     "missing signing key",
			env:      map[string]string{"DATABASE_URL": dbURL},
			wantErrs: []string{"AUTH_JWT_SIGNING_KEY"},
		},
		{
			name:     "shared and service errors together",
			env:      map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey, "APP_ENV": "qa", "AUTH_ARGON2_PARALLELISM": "0"},
			wantErrs: []string{"APP_ENV", "AUTH_ARGON2_PARALLELISM"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.env["AUTH_ACTION_TOKEN_KEY"] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			cfg, err := config.Parse(tt.env)
			if len(tt.wantErrs) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErrs {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ":8080", cfg.HTTP.Addr)
			assert.Equal(t, uint32(19456), cfg.Auth.Argon2MemoryKiB)
			assert.Len(t, cfg.Auth.SigningKeySeed(), 32)
		})
	}
}

func TestProductionConfig(t *testing.T) {
	valid := map[string]string{"DATABASE_URL": dbURL, "AUTH_JWT_SIGNING_KEY": signingKey, "AUTH_ACTION_TOKEN_KEY": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), "APP_ENV": "prod", "EVENTS_BACKEND": "aws", "EVENTS_TOPIC_ARN": "arn:aws:sns:us-east-1:123456789012:events", "EVENTS_QUEUE_URL": "https://sqs.us-east-1.amazonaws.com/123456789012/worker", "MAILER_BACKEND": "ses", "AUTH_ISSUER": "scaffold", "AUTH_LINK_BASE_URL": "https://console.example.com"}
	_, err := config.Parse(valid)
	require.NoError(t, err)
	tests := []struct{ key, value string }{
		{"EVENTS_BACKEND", "memory"}, {"MAILER_BACKEND", "memory"}, {"MAILER_BACKEND", "smtp"},
		{"EVENTS_AWS_ENDPOINT_URL", "http://localhost:4566"}, {"AUTH_ISSUER", ""},
		{"AUTH_LINK_BASE_URL", "http://console.example.com"}, {"AUTH_LINK_BASE_URL", "https://127.0.0.1"}, {"AUTH_LINK_BASE_URL", "https://[::1]"}, {"AUTH_LINK_BASE_URL", "https://localhost"},
		{"AUTH_ACTION_TOKEN_KEY", ""}, {"AUTH_ACTION_TOKEN_KEY", signingKey}, {"AUTH_RECIPIENT_COOLDOWN", "0s"},
		{"RATELIMIT_SESSION_LIMIT", "0"}, {"EVENTS_HANDLER_TIMEOUT", "0s"}, {"OUTBOX_PUBLISH_TIMEOUT", "0s"}, {"OIDC_DISCOVERY_TIMEOUT", "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			changed := make(map[string]string, len(valid))
			for k, v := range valid {
				changed[k] = v
			}
			changed[tt.key] = tt.value
			_, err := config.Parse(changed)
			require.Error(t, err)
		})
	}
}
