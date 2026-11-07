package config_test

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/config"
)

var valid = map[string]string{
	"DELIVERY_DATABASE_URL":  "postgres://u:p@localhost:5433/hookbeam_delivery",
	"DELIVERY_TLS_CERT_FILE": "deploy/certs/server.crt",
	"DELIVERY_TLS_KEY_FILE":  "deploy/certs/server.key",
	"DELIVERY_TLS_CA_FILE":   "deploy/certs/ca.crt",
	"DELIVERY_SECRETS_KEY":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
}

func with(overrides map[string]string) map[string]string {
	m := maps.Clone(valid)
	maps.Copy(m, overrides)
	return m
}

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantErrs []string
	}{
		{name: "defaults"},
		{name: "insecure in dev", env: with(map[string]string{"DELIVERY_TLS_INSECURE": "true", "DELIVERY_TLS_CERT_FILE": "", "DELIVERY_TLS_KEY_FILE": "", "DELIVERY_TLS_CA_FILE": ""})},
		{name: "missing database", env: with(map[string]string{"DELIVERY_DATABASE_URL": ""}), wantErrs: []string{"DELIVERY_DATABASE_URL is required"}},
		{name: "missing tls files", env: with(map[string]string{"DELIVERY_TLS_CA_FILE": ""}), wantErrs: []string{"DELIVERY_TLS_CA_FILE is required unless DELIVERY_TLS_INSECURE=true"}},
		{name: "bad secrets key", env: with(map[string]string{"DELIVERY_SECRETS_KEY": "short"}), wantErrs: []string{"DELIVERY_SECRETS_KEY must be base64 of 32 random bytes"}},
		{name: "bad events backend and outbox", env: with(map[string]string{"DELIVERY_EVENTS_BACKEND": "nats", "DELIVERY_OUTBOX_BATCH_SIZE": "0"}), wantErrs: []string{"DELIVERY_EVENTS_BACKEND must be aws or memory", "DELIVERY_OUTBOX_POLL_INTERVAL"}},
		{name: "claims and sender bounds", env: with(map[string]string{"DELIVERY_CLAIM_BATCH_SIZE": "0", "DELIVERY_SENDER_TIMEOUT": "0s", "DELIVERY_PAYLOAD_MAX_BYTES": "0"}), wantErrs: []string{"DELIVERY_CLAIM_INTERVAL", "DELIVERY_SENDER_TIMEOUT", "DELIVERY_PAYLOAD_MAX_BYTES"}},
		{name: "shared and own errors together", env: with(map[string]string{"APP_ENV": "qa", "LOG_LEVEL": "loud", "DELIVERY_GRPC_SHUTDOWN_TIMEOUT": "0s"}), wantErrs: []string{"APP_ENV must be dev, staging or prod", "LOG_LEVEL must be", "DELIVERY_GRPC_SHUTDOWN_TIMEOUT must be positive"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := tt.env
			if env == nil {
				env = valid
			}
			cfg, err := config.Parse(env)
			if len(tt.wantErrs) == 0 {
				require.NoError(t, err)
				require.Equal(t, ":9090", cfg.GRPC.Addr)
				require.Equal(t, "memory", cfg.Events.Backend)
				require.Equal(t, "2026-11", cfg.Secrets.KeyID)
				require.Equal(t, []int{443, 8443}, cfg.Sender.AllowedPorts)
				require.Equal(t, 20, cfg.Claims.BatchSize)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErrs {
				require.ErrorContains(t, err, want)
			}
		})
	}
}

func TestProductionConfig(t *testing.T) {
	prod := with(map[string]string{"APP_ENV": "prod", "DELIVERY_EVENTS_BACKEND": "aws", "DELIVERY_EVENTS_TOPIC_ARN": "arn", "DELIVERY_EVENTS_QUEUE_URL": "https://sqs"})
	_, err := config.Parse(prod)
	require.NoError(t, err)

	_, err = config.Parse(with(map[string]string{"APP_ENV": "prod", "DELIVERY_TLS_INSECURE": "true", "DELIVERY_EVENTS_BACKEND": "aws", "DELIVERY_EVENTS_TOPIC_ARN": "arn", "DELIVERY_EVENTS_QUEUE_URL": "https://sqs"}))
	require.ErrorContains(t, err, "DELIVERY_TLS_INSECURE is not allowed")

	_, err = config.Parse(with(map[string]string{"APP_ENV": "staging"}))
	require.ErrorContains(t, err, "staging/prod require AWS events")

	prod["DELIVERY_SENDER_ALLOW_PRIVATE"] = "true"
	_, err = config.Parse(prod)
	require.ErrorContains(t, err, "DELIVERY_SENDER_ALLOW_PRIVATE is for local development only")
}
