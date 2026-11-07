package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/config"
)

func TestEventsValidate(t *testing.T) {
	ok := config.Events{Backend: "memory", HandlerTimeout: time.Minute}
	require.NoError(t, ok.Validate("EVENTS_", false))
	require.ErrorContains(t, config.Events{Backend: "nats", HandlerTimeout: time.Minute}.Validate("EVENTS_", false), "EVENTS_BACKEND must be aws or memory")
	require.ErrorContains(t, config.Events{Backend: "aws", HandlerTimeout: time.Minute}.Validate("X_", false), "X_TOPIC_ARN and X_QUEUE_URL are required")
	require.ErrorContains(t, config.Events{Backend: "memory"}.Validate("EVENTS_", false), "EVENTS_HANDLER_TIMEOUT must be positive")
	require.ErrorContains(t, ok.Validate("EVENTS_", true), "staging/prod require AWS events")
	aws := config.Events{Backend: "aws", TopicARN: "arn", QueueURL: "url", HandlerTimeout: time.Minute}
	require.NoError(t, aws.Validate("EVENTS_", true))
	aws.Endpoint = "http://localhost:4566"
	require.Error(t, aws.Validate("EVENTS_", true))
}

func TestOutboxValidate(t *testing.T) {
	require.NoError(t, config.Outbox{PublishTimeout: time.Second, PollInterval: time.Second, BatchSize: 1, Retention: time.Hour}.Validate("OUTBOX_"))
	require.ErrorContains(t, config.Outbox{PublishTimeout: time.Second, PollInterval: time.Second, Retention: time.Hour}.Validate("OUTBOX_"), "OUTBOX_POLL_INTERVAL, OUTBOX_BATCH_SIZE, OUTBOX_RETENTION and OUTBOX_PUBLISH_TIMEOUT must be positive")
}
