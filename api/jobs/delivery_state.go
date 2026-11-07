package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/events"
)

// NewDeliveryState returns the handler for every delivery state event: it feeds the endpoint health projection.
func NewDeliveryState(health services.EndpointHealthService) Handler {
	return func(ctx context.Context, e events.Event) error {
		var p apievents.DeliveryStatePayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
		}
		return health.Observe(ctx, p)
	}
}
