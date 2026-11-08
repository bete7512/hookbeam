package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/pkg/events"
)

// NewEndpointHealthChanged returns the handler for endpoint health changes. Until notifications exist it
// only validates the payload and logs the change, so the event is consumed instead of dead-lettered.
func NewEndpointHealthChanged(log *slog.Logger) Handler {
	return func(ctx context.Context, e events.Event) error {
		var p apievents.EndpointHealthChangedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
		}
		if p.EndpointID <= 0 || p.EpisodeID <= 0 || p.State == "" {
			return fmt.Errorf("jobs: %s payload is incomplete", e.Type)
		}
		log.InfoContext(ctx, "endpoint health changed", "workspace_id", p.WorkspaceID, "endpoint_id", p.EndpointID, "episode_id", p.EpisodeID, "state", p.State, "consecutive_failures", p.ConsecutiveFailures)
		return nil
	}
}
