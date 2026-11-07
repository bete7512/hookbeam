package jobs_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/jobs"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/events"
)

func TestDeliveryState(t *testing.T) {
	health := service_mocks.NewMockEndpointHealthService(gomock.NewController(t))
	h := jobs.NewDeliveryState(health)
	payload, err := json.Marshal(apievents.DeliveryStatePayload{WorkspaceID: "ws_1", EndpointID: 3, DeliveryID: 5, Attempt: 1, State: "succeeded", OccurredAt: time.Now()})
	require.NoError(t, err)

	health.EXPECT().Observe(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, p apievents.DeliveryStatePayload) error {
		require.Equal(t, int64(3), p.EndpointID)
		require.Equal(t, "succeeded", p.State)
		return nil
	})
	require.NoError(t, h(context.Background(), events.Event{ID: 1, Type: apievents.DeliverySucceeded, Payload: payload}))

	require.Error(t, h(context.Background(), events.Event{ID: 2, Type: apievents.DeliverySucceeded, Payload: []byte("not json")}), "a malformed payload is retained for the dead-letter queue")
}
