// Package events names the data plane's event types and their payloads.
package events

// DeliveryDue wakes workers: deliveries of the message were planned or rescheduled. Workers claim
// whatever is due, so a duplicate or late wake-up is harmless.
const DeliveryDue = "delivery.due"

// DeliveryDuePayload is the payload of DeliveryDue.
type DeliveryDuePayload struct {
	MessageID int64 `json:"message_id"`
}
