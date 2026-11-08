// Package events names the control plane's event types and their payloads.
// Account emails are sent by goauth's notification module, not through the outbox.
// The delivery state events below are produced by the data plane; this is the control plane's own copy
// of their names and shape, kept compatible by hand (services never import each other).
package events

import "time"

// Delivery state events announce what happened to a delivery.
const (
	DeliverySucceeded      = "delivery.succeeded"
	DeliveryFailed         = "delivery.failed"
	DeliveryCancelled      = "delivery.cancelled"
	DeliveryRetryScheduled = "delivery.retry_scheduled"
)

// EndpointHealthChanged is the data plane's announcement that an endpoint's health episode opened or resolved.
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

// DeliveryStateTypes lists every delivery state event, for handler registration.
var DeliveryStateTypes = []string{DeliverySucceeded, DeliveryFailed, DeliveryCancelled, DeliveryRetryScheduled}

// DeliveryStatePayload is the payload of every delivery state event.
type DeliveryStatePayload struct {
	WorkspaceID   string    `json:"workspace_id"`
	ApplicationID int64     `json:"application_id"`
	EndpointID    int64     `json:"endpoint_id"`
	MessageID     int64     `json:"message_id"`
	DeliveryID    int64     `json:"delivery_id"`
	Attempt       int32     `json:"attempt"`
	State         string    `json:"state"`
	Reason        *string   `json:"reason,omitempty"`
	StatusCode    *int32    `json:"status_code,omitempty"`
	ErrorClass    *string   `json:"error_class,omitempty"`
	OccurredAt    time.Time `json:"occurred_at"`
}
