package jobs_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/jobs"
	"github.com/bete7512/hookbeam/pkg/events"
)

func TestEndpointHealthChanged(t *testing.T) {
	h := jobs.NewEndpointHealthChanged(slog.New(slog.DiscardHandler))
	payload, err := json.Marshal(apievents.EndpointHealthChangedPayload{WorkspaceID: "ws_1", EndpointID: 3, EpisodeID: 9, Revision: 2, State: "unhealthy", ConsecutiveFailures: 3, OccurredAt: time.Now()})
	require.NoError(t, err)
	require.NoError(t, h(context.Background(), events.Event{ID: 1, Type: apievents.EndpointHealthChanged, Payload: payload}))

	require.Error(t, h(context.Background(), events.Event{ID: 2, Type: apievents.EndpointHealthChanged, Payload: []byte("not json")}), "a malformed payload is retained for the dead-letter queue")
	require.Error(t, h(context.Background(), events.Event{ID: 3, Type: apievents.EndpointHealthChanged, Payload: []byte(`{"endpoint_id":3}`)}), "an incomplete payload is refused")
}
