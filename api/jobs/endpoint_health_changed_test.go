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

func TestEndpointHealthChanged(t *testing.T) {
	notifications := service_mocks.NewMockNotificationService(gomock.NewController(t))
	h := jobs.NewEndpointHealthChanged(notifications)
	payload, err := json.Marshal(apievents.EndpointHealthChangedPayload{WorkspaceID: "ws_1", EndpointID: 3, EpisodeID: 9, Revision: 2, State: "unhealthy", ConsecutiveFailures: 3, OccurredAt: time.Now()})
	require.NoError(t, err)
	notifications.EXPECT().HandleHealthChange(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, p apievents.EndpointHealthChangedPayload) error {
		require.Equal(t, int64(9), p.EpisodeID)
		require.Equal(t, "unhealthy", p.State)
		return nil
	})
	require.NoError(t, h(context.Background(), events.Event{ID: 1, Type: apievents.EndpointHealthChanged, Payload: payload}))

	require.Error(t, h(context.Background(), events.Event{ID: 2, Type: apievents.EndpointHealthChanged, Payload: []byte("not json")}), "a malformed payload is retained for the dead-letter queue")
}

func TestNotificationEmail(t *testing.T) {
	notifications := service_mocks.NewMockNotificationService(gomock.NewController(t))
	h := jobs.NewNotificationEmail(notifications)
	payload, err := json.Marshal(apievents.NotificationEmailPayload{NotificationID: 12, UserID: "u_ada"})
	require.NoError(t, err)
	notifications.EXPECT().SendEmail(gomock.Any(), int64(12)).Return(nil)
	require.NoError(t, h(context.Background(), events.Event{ID: 1, Type: apievents.NotificationEmailRequested, Payload: payload}))

	require.Error(t, h(context.Background(), events.Event{ID: 2, Type: apievents.NotificationEmailRequested, Payload: []byte("{}")}), "an incomplete payload is refused")
	require.Error(t, h(context.Background(), events.Event{ID: 3, Type: apievents.NotificationEmailRequested, Payload: []byte("x")}))
}
