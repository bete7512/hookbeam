package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/jobs"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/events"
)

const testEvent = "test.happened"

// mocks are the service mocks behind a Jobs under test; calls counts handler runs.
type mocks struct {
	events *service_mocks.MockEventService
	calls  int
	fail   error
}

func newJobs(t *testing.T) (*jobs.Jobs, *mocks) {
	return newJobsWithTimeout(t, 0)
}

func newJobsWithTimeout(t *testing.T, timeout time.Duration) (*jobs.Jobs, *mocks) {
	t.Helper()
	m := &mocks{events: service_mocks.NewMockEventService(gomock.NewController(t))}
	handler := func(context.Context, events.Event) error {
		m.calls++
		return m.fail
	}
	j, err := jobs.New(jobs.Deps{Events: m.events, Logger: slog.New(slog.DiscardHandler), HandlerTimeout: timeout, Handlers: map[string]jobs.Handler{testEvent: handler}})
	require.NoError(t, err)
	return j, m
}

func TestNew(t *testing.T) {
	_, err := jobs.New(jobs.Deps{Logger: slog.New(slog.DiscardHandler)})
	require.Error(t, err, "Events is required")
	_, err = jobs.New(jobs.Deps{Events: service_mocks.NewMockEventService(gomock.NewController(t)), Logger: slog.New(slog.DiscardHandler), Handlers: map[string]jobs.Handler{"": nil}})
	require.Error(t, err, "handlers need a type and a function")
}

func TestHandle(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name        string
		eventType   string
		fresh       bool
		fail        error
		wantCalls   int
		wantErr     error
		wantErrText string
	}{
		{name: "runs the handler for a new event", eventType: testEvent, fresh: true, wantCalls: 1},
		{name: "skips the handler on a replay", eventType: testEvent, fresh: false},
		{name: "handler error is returned for redelivery", eventType: testEvent, fresh: true, fail: boom, wantCalls: 1, wantErr: boom},
		{name: "unknown type is retained without touching the service", eventType: "unknown", wantErrText: "no handler for event type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, m := newJobs(t)
			m.fail = tt.fail
			if tt.eventType == testEvent {
				m.events.EXPECT().ProcessOnce(gomock.Any(), int64(7), gomock.Any()).DoAndReturn(
					func(ctx context.Context, _ int64, fn func(context.Context) error) (bool, error) {
						if !tt.fresh {
							return false, nil
						}
						if err := fn(ctx); err != nil {
							return false, err
						}
						return true, nil
					})
			}

			err := j.Handle(context.Background(), events.Event{ID: 7, Type: tt.eventType, Payload: []byte(`{}`)})

			require.Equal(t, tt.wantCalls, m.calls)
			if tt.wantErrText != "" {
				require.ErrorContains(t, err, tt.wantErrText)
				return
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestHandleInvalidEnvelope(t *testing.T) {
	for _, e := range []events.Event{
		{Type: testEvent},
		{ID: -1, Type: testEvent},
		{ID: 7},
	} {
		j, _ := newJobs(t)
		require.Error(t, j.Handle(context.Background(), e))
	}
}

func TestHandleTimeout(t *testing.T) {
	j, m := newJobsWithTimeout(t, 20*time.Millisecond)
	m.events.EXPECT().ProcessOnce(gomock.Any(), int64(7), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ int64, _ func(context.Context) error) (bool, error) {
			_, ok := ctx.Deadline()
			require.True(t, ok)
			<-ctx.Done()
			return false, ctx.Err()
		})
	err := j.Handle(context.Background(), events.Event{ID: 7, Type: testEvent})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
