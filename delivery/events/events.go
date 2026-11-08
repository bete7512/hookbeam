// Package events names the data plane's event types and their payloads.
package events

import "time"

// DeliveryDue wakes workers: deliveries of the message were planned or rescheduled. Workers claim
// whatever is due, so a duplicate or late wake-up is harmless.
const DeliveryDue = "delivery.due"

// DeliveryDuePayload is the payload of DeliveryDue.
type DeliveryDuePayload struct {
	MessageID int64 `json:"message_id"`
}

// Delivery state events tell the control plane what happened to a delivery; the bus carries them to its
// projections and notifications. The names are the delivery states they announce.
const (
	DeliverySucceeded      = "delivery.succeeded"
	DeliveryFailed         = "delivery.failed"
	DeliveryCancelled      = "delivery.cancelled"
	DeliveryRetryScheduled = "delivery.retry_scheduled"
)

// DeliveryStatePayload is the payload of every delivery state event. The control plane keeps its own copy
// of this shape; the two must stay compatible.
type DeliveryStatePayload struct {
	WorkspaceID   string    `json:"workspace_id"`
	ApplicationID int64     `json:"application_id"`
	EndpointID    int64     `json:"endpoint_id"`
	MessageID     int64     `json:"message_id"`
	DeliveryID    int64     `json:"delivery_id"`
	Attempt       int32     `json:"attempt"` // 0 when no attempt was made (a cancel before the first claim)
	State         string    `json:"state"`
	Reason        *string   `json:"reason,omitempty"`
	StatusCode    *int32    `json:"status_code,omitempty"`
	ErrorClass    *string   `json:"error_class,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}

// EndpointHealthChanged announces that an endpoint's health episode opened (state unhealthy) or resolved (healthy).
const EndpointHealthChanged = "endpoint.health_changed"

// EndpointHealthChangedPayload is the payload of EndpointHealthChanged.
type EndpointHealthChangedPayload struct {
	WorkspaceID         string    `json:"workspace_id"`
	ApplicationID       int64     `json:"application_id"`
	EndpointID          int64     `json:"endpoint_id"`
	EpisodeID           int64     `json:"episode_id"`
	Revision            int32     `json:"revision"`
	State               string    `json:"state"` // healthy or unhealthy
	ConsecutiveFailures int32     `json:"consecutive_failures"`
	OccurredAt          time.Time `json:"occurred_at"`
}

// DeliveryStateEvent names the event type for a delivery state, or "" for a state nothing announces.
func DeliveryStateEvent(state string) string {
	switch state {
	case "succeeded":
		return DeliverySucceeded
	case "failed":
		return DeliveryFailed
	case "cancelled":
		return DeliveryCancelled
	case "retry_scheduled":
		return DeliveryRetryScheduled
	}
	return ""
}
