package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/events"
)

// NewEndpointHealthChanged returns the handler for endpoint health changes: it turns them into notifications.
func NewEndpointHealthChanged(notifications services.NotificationService) Handler {
	return func(ctx context.Context, e events.Event) error {
		var p apievents.EndpointHealthChangedPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
		}
		return notifications.HandleHealthChange(ctx, p)
	}
}

// NewNotificationEmail returns the handler that mails one stored notification.
func NewNotificationEmail(notifications services.NotificationService) Handler {
	return func(ctx context.Context, e events.Event) error {
		var p apievents.NotificationEmailPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
		}
		if p.NotificationID <= 0 {
			return fmt.Errorf("jobs: %s payload is incomplete", e.Type)
		}
		return notifications.SendEmail(ctx, p.NotificationID)
	}
}
