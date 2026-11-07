// Package memory is an in-process events transport for tests and local runs without a queue.
package memory

import (
	"context"

	"github.com/bete7512/hookbeam/pkg/events"
)

// Bus is a Publisher and Subscriber over one channel. A handler error redelivers the event once.
type Bus struct {
	ch chan events.Event
}

// New returns a Bus buffering up to size events.
func New(size int) *Bus {
	return &Bus{ch: make(chan events.Event, size)}
}

// Publish queues events for Run.
func (b *Bus) Publish(ctx context.Context, evs []events.Event) error {
	for _, e := range evs {
		select {
		case b.ch <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Run delivers queued events to h until ctx ends.
func (b *Bus) Run(ctx context.Context, h events.Handler) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-b.ch:
			if err := h(ctx, e); err != nil {
				_ = h(ctx, e)
			}
		}
	}
}

// Len reports how many events are waiting.
func (b *Bus) Len() int { return len(b.ch) }
