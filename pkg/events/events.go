// Package events defines the event envelope and the Publisher/Subscriber contract every transport implements.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Event is the envelope carried through the outbox and the queue.
type Event struct {
	ID         int64           `json:"id"`
	Type       string          `json:"type"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// Validate checks the fields required to identify and dispatch a delivered event.
func (e Event) Validate() error {
	if e.ID <= 0 || strings.TrimSpace(e.Type) == "" {
		return errors.New("events: positive id and nonempty type are required")
	}
	return nil
}

// New builds an Event of the given type; payload is marshalled to JSON.
func New(eventType string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: eventType, OccurredAt: time.Now(), Payload: raw}, nil
}

// Publisher sends events to the transport.
type Publisher interface {
	Publish(ctx context.Context, events []Event) error
}

// Handler processes one delivered event. A nil return acks it; an error leaves it for redelivery.
type Handler func(ctx context.Context, e Event) error

// Subscriber delivers events to a Handler until ctx ends.
type Subscriber interface {
	Run(ctx context.Context, h Handler) error
}
