package transport_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/config"
	"github.com/bete7512/hookbeam/pkg/events/transport"
)

func TestNew(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	pub, sub, err := transport.New(context.Background(), config.Events{Backend: "memory"}, log)
	require.NoError(t, err)
	require.NotNil(t, pub)
	require.NotNil(t, sub)

	_, _, err = transport.New(context.Background(), config.Events{Backend: "nats"}, log)
	require.ErrorContains(t, err, `unknown events backend "nats"`)
}
