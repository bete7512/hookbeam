//go:build integration

package sqs_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/events/sns"
	"github.com/bete7512/scaffold/pkg/events/sqs"
)

type fixture struct {
	pub      *sns.Publisher
	sqs      *awssqs.Client
	queueURL string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	endpoint := os.Getenv("EVENTS_AWS_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("EVENTS_AWS_ENDPOINT_URL not set; start LocalStack with `make queue-up`")
	}
	for k, v := range map[string]string{
		"EVENTS_AWS_REGION":     "us-east-1",
		"AWS_ACCESS_KEY_ID":     "test",
		"AWS_SECRET_ACCESS_KEY": "test",
	} {
		if os.Getenv(k) == "" {
			t.Setenv(k, v)
		}
	}
	region := os.Getenv("EVENTS_AWS_REGION")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snsClient, err := sns.NewClient(ctx, region, endpoint)
	require.NoError(t, err)
	sqsClient, err := sqs.NewClient(ctx, region, endpoint)
	require.NoError(t, err)
	name := "scaffold-test-" + strings.ToLower(rand.Text())
	topic, err := snsClient.CreateTopic(ctx, &awssns.CreateTopicInput{Name: aws.String(name)})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := snsClient.DeleteTopic(ctx, &awssns.DeleteTopicInput{TopicArn: topic.TopicArn})
		assert.NoError(t, err)
	})
	queue, err := sqsClient.CreateQueue(ctx, &awssqs.CreateQueueInput{QueueName: aws.String(name)})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := sqsClient.DeleteQueue(ctx, &awssqs.DeleteQueueInput{QueueUrl: queue.QueueUrl})
		assert.NoError(t, err)
	})
	attributes, err := sqsClient.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl: queue.QueueUrl, AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	require.NoError(t, err)
	queueARN := attributes.Attributes[string(types.QueueAttributeNameQueueArn)]
	policy, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow", "Principal": map[string]string{"Service": "sns.amazonaws.com"},
			"Action": "sqs:SendMessage", "Resource": queueARN,
			"Condition": map[string]any{"ArnEquals": map[string]string{"aws:SourceArn": aws.ToString(topic.TopicArn)}},
		}},
	})
	require.NoError(t, err)
	_, err = sqsClient.SetQueueAttributes(ctx, &awssqs.SetQueueAttributesInput{
		QueueUrl: queue.QueueUrl, Attributes: map[string]string{string(types.QueueAttributeNamePolicy): string(policy)},
	})
	require.NoError(t, err)
	subscription, err := snsClient.Subscribe(ctx, &awssns.SubscribeInput{
		TopicArn: topic.TopicArn, Protocol: aws.String("sqs"), Endpoint: aws.String(queueARN),
		Attributes: map[string]string{"RawMessageDelivery": "true"},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := snsClient.Unsubscribe(ctx, &awssns.UnsubscribeInput{SubscriptionArn: subscription.SubscriptionArn})
		assert.NoError(t, err)
	})
	return &fixture{pub: sns.NewPublisher(snsClient, aws.ToString(topic.TopicArn)), sqs: sqsClient, queueURL: aws.ToString(queue.QueueUrl)}
}

func mustEvent(t *testing.T, id int64, typ string, payload any) events.Event {
	t.Helper()
	e, err := events.New(typ, payload)
	require.NoError(t, err)
	e.ID = id
	return e
}

func TestPublishSubscribe(t *testing.T) {
	tests := []struct {
		name       string
		events     []events.Event
		failFirst  bool
		wantTypes  []string
		visibility time.Duration
	}{
		{
			name: "delivers each published event once",
			events: []events.Event{
				mustEvent(t, 1, "user.registered", map[string]any{"id": 1, "email": "ada@example.com"}),
				mustEvent(t, 2, "user.deleted", map[string]any{"id": 2}),
			},
			wantTypes:  []string{"user.registered", "user.deleted"},
			visibility: 30 * time.Second,
		},
		{
			name:       "redelivers after a handler error",
			events:     []events.Event{mustEvent(t, 3, "user.registered", map[string]any{"id": 3})},
			failFirst:  true,
			wantTypes:  []string{"user.registered", "user.registered"},
			visibility: time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			require.NoError(t, f.pub.Publish(ctx, tt.events))

			delivered := make(chan events.Event, len(tt.wantTypes))
			var calls atomic.Int32
			handler := func(_ context.Context, e events.Event) error {
				delivered <- e
				if tt.failFirst && calls.Add(1) == 1 {
					return errors.New("transient")
				}
				return nil
			}
			sub := sqs.NewSubscriber(f.sqs, f.queueURL, sqs.WithVisibilityTimeout(tt.visibility))
			runErr := make(chan error, 1)
			go func() { runErr <- sub.Run(ctx, handler) }()

			got := make([]events.Event, 0, len(tt.wantTypes))
			for len(got) < len(tt.wantTypes) {
				select {
				case e := <-delivered:
					got = append(got, e)
				case <-ctx.Done():
					require.FailNowf(t, "timed out", "got %d of %d deliveries", len(got), len(tt.wantTypes))
				}
			}
			require.Eventually(t, func() bool {
				out, err := f.sqs.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
					QueueUrl: aws.String(f.queueURL), AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameApproximateNumberOfMessagesNotVisible},
				})
				return err == nil && out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessagesNotVisible)] == "0"
			}, 5*time.Second, 20*time.Millisecond, "wait for successful acknowledgement before shutting down")
			cancel()
			require.NoError(t, <-runErr)

			gotTypes := make([]string, 0, len(got))
			for _, e := range got {
				gotTypes = append(gotTypes, e.Type)
			}
			assert.ElementsMatch(t, tt.wantTypes, gotTypes)
			byType := map[string]events.Event{}
			for _, e := range tt.events {
				byType[e.Type] = e
			}
			for _, e := range got {
				want := byType[e.Type]
				assert.JSONEq(t, string(want.Payload), string(e.Payload))
				assert.True(t, want.OccurredAt.Equal(e.OccurredAt), "occurred_at round-trips")
			}
		})
	}
}
