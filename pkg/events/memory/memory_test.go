package memory_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/memory"
)

func TestBus(t *testing.T) {
	tests := []struct {
		name      string
		failFirst bool
		wantCalls int32
	}{
		{name: "delivers once", wantCalls: 1},
		{name: "redelivers once after a handler error", failFirst: true, wantCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus := memory.New(8)
			e, err := events.New("thing.happened", map[string]int{"id": 1})
			require.NoError(t, err)
			require.NoError(t, bus.Publish(context.Background(), []events.Event{e}))

			var calls atomic.Int32
			done := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				_ = bus.Run(ctx, func(_ context.Context, got events.Event) error {
					assert.Equal(t, "thing.happened", got.Type)
					if calls.Add(1) == 1 && tt.failFirst {
						return errors.New("boom")
					}
					close(done)
					return nil
				})
			}()

			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("event was not delivered")
			}
			cancel()
			assert.Equal(t, tt.wantCalls, calls.Load())
		})
	}
}
