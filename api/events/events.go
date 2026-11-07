// Package events names the control plane's event types and their payloads.
// Account emails are sent by goauth's notification module, not through the outbox;
// delivery-domain events are added here as the data plane arrives.
package events
