package sqs

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/events"
)

type stubClient struct {
	receive func(context.Context, *awssqs.ReceiveMessageInput) (*awssqs.ReceiveMessageOutput, error)
	delete  func(context.Context, *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error)
	change  func(context.Context, *awssqs.ChangeMessageVisibilityInput) (*awssqs.ChangeMessageVisibilityOutput, error)
}

func (s stubClient) ReceiveMessage(ctx context.Context, in *awssqs.ReceiveMessageInput, _ ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error) {
	return s.receive(ctx, in)
}

func (s stubClient) DeleteMessage(ctx context.Context, in *awssqs.DeleteMessageInput, _ ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error) {
	return s.delete(ctx, in)
}

func (s stubClient) ChangeMessageVisibility(ctx context.Context, in *awssqs.ChangeMessageVisibilityInput, _ ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error) {
	return s.change(ctx, in)
}

func message(body string) types.Message {
	return types.Message{Body: aws.String(body), MessageId: aws.String("message"), ReceiptHandle: aws.String("receipt")}
}

func TestHandle(t *testing.T) {
	for _, tt := range []struct {
		name        string
		body        string
		handlerErr  error
		wantCalls   int
		wantDeletes int
	}{
		{name: "successful processing acknowledges", body: `{"id":1,"type":"known"}`, wantCalls: 1, wantDeletes: 1},
		{name: "failed processing remains for redelivery", body: `{"id":1,"type":"known"}`, handlerErr: errors.New("transient"), wantCalls: 1},
		{name: "malformed JSON remains for DLQ", body: `{`},
		{name: "missing ID remains for DLQ", body: `{"type":"known"}`},
		{name: "negative ID remains for DLQ", body: `{"id":-1,"type":"known"}`},
		{name: "missing type remains for DLQ", body: `{"id":1}`},
		{name: "blank type remains for DLQ", body: `{"id":1,"type":" "}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls, deletes := 0, 0
			client := stubClient{delete: func(ctx context.Context, in *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error) {
				deletes++
				require.Equal(t, "receipt", aws.ToString(in.ReceiptHandle))
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				return &awssqs.DeleteMessageOutput{}, nil
			}}
			sub := NewSubscriber(client, "queue", WithLogger(slog.New(slog.DiscardHandler)))
			sub.handle(context.Background(), func(ctx context.Context, _ events.Event) error {
				calls++
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				return tt.handlerErr
			}, message(tt.body))
			require.Equal(t, tt.wantCalls, calls)
			require.Equal(t, tt.wantDeletes, deletes)
		})
	}
}

func TestHandleTimeout(t *testing.T) {
	var deleted bool
	client := stubClient{delete: func(context.Context, *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error) {
		deleted = true
		return &awssqs.DeleteMessageOutput{}, nil
	}}
	sub := NewSubscriber(client, "queue", WithLogger(slog.New(slog.DiscardHandler)), WithHandlerTimeout(20*time.Millisecond))
	sub.handle(context.Background(), func(ctx context.Context, _ events.Event) error {
		<-ctx.Done()
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		return nil // A late nil result must not acknowledge a timed-out handler.
	}, message(`{"id":1,"type":"known"}`))
	require.False(t, deleted)
}

func TestHandleVisibility(t *testing.T) {
	for _, tt := range []struct {
		name  string
		fail  bool
		stall bool
	}{
		{name: "renews while processing"},
		{name: "lease failure cancels processing", fail: true},
		{name: "stalled renewal cancels processing", stall: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var renewals atomic.Int32
			var deleted bool
			renewed := make(chan struct{}, 4)
			client := stubClient{
				change: func(ctx context.Context, in *awssqs.ChangeMessageVisibilityInput) (*awssqs.ChangeMessageVisibilityOutput, error) {
					renewals.Add(1)
					if aws.ToString(in.ReceiptHandle) != "receipt" || in.VisibilityTimeout != 1 {
						return nil, errors.New("unexpected renewal")
					}
					if tt.fail {
						return nil, errors.New("lease lost")
					}
					if tt.stall {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					renewed <- struct{}{}
					return &awssqs.ChangeMessageVisibilityOutput{}, nil
				},
				delete: func(context.Context, *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error) {
					deleted = true
					return &awssqs.DeleteMessageOutput{}, nil
				},
			}
			sub := NewSubscriber(client, "queue", WithLogger(slog.New(slog.DiscardHandler)), WithVisibilityTimeout(time.Second), WithHandlerTimeout(3*time.Second))
			sub.handle(context.Background(), func(ctx context.Context, _ events.Event) error {
				if tt.fail || tt.stall {
					<-ctx.Done()
					require.ErrorIs(t, ctx.Err(), context.Canceled)
					return nil
				}
				for range 2 {
					select {
					case <-renewed:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}, message(`{"id":1,"type":"known"}`))
			require.Equal(t, !tt.fail && !tt.stall, deleted)
			require.Positive(t, renewals.Load())
		})
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received, handled, deleted := 0, 0, 0
	client := stubClient{
		receive: func(ctx context.Context, in *awssqs.ReceiveMessageInput) (*awssqs.ReceiveMessageOutput, error) {
			require.Equal(t, int32(1), in.MaxNumberOfMessages, "do not lease messages beyond processing capacity")
			require.Equal(t, received, handled, "finish the current message before receiving another")
			_, bounded := ctx.Deadline()
			require.True(t, bounded)
			if received == 2 {
				cancel()
				return nil, context.Canceled
			}
			received++
			return &awssqs.ReceiveMessageOutput{Messages: []types.Message{message(`{"id":1,"type":"known"}`)}}, nil
		},
		delete: func(context.Context, *awssqs.DeleteMessageInput) (*awssqs.DeleteMessageOutput, error) {
			deleted++
			return &awssqs.DeleteMessageOutput{}, nil
		},
	}
	sub := NewSubscriber(client, "queue", WithLogger(slog.New(slog.DiscardHandler)))
	require.NoError(t, sub.Run(ctx, func(context.Context, events.Event) error {
		handled++
		return nil
	}))
	require.Equal(t, 2, deleted)
}
