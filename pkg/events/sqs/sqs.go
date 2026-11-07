// Package sqs delivers events from an Amazon SQS queue to a handler.
package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/bete7512/hookbeam/pkg/events"
)

const (
	receiveBatchSize      = 1
	receiveWait           = 20 * time.Second
	receiveRetryDelay     = time.Second
	requestTimeout        = 5 * time.Second
	defaultVisibility     = 30 * time.Second
	defaultHandlerTimeout = 2 * time.Minute
)

type sqsAPI interface {
	ReceiveMessage(context.Context, *awssqs.ReceiveMessageInput, ...func(*awssqs.Options)) (*awssqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *awssqs.DeleteMessageInput, ...func(*awssqs.Options)) (*awssqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *awssqs.ChangeMessageVisibilityInput, ...func(*awssqs.Options)) (*awssqs.ChangeMessageVisibilityOutput, error)
}

// NewClient builds an SQS client for region from the default credential chain; endpoint overrides the AWS URL (LocalStack).
func NewClient(ctx context.Context, region, endpoint string) (*awssqs.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("sqs: load aws config: %w", err)
	}
	return awssqs.NewFromConfig(cfg, func(o *awssqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

// Subscriber delivers messages of one queue to a Handler.
type Subscriber struct {
	client         sqsAPI
	queueURL       string
	logger         *slog.Logger
	visibility     time.Duration
	handlerTimeout time.Duration
}

// Option configures a Subscriber.
type Option func(*Subscriber)

// WithLogger sets the logger used for receive, decode and handler failures.
func WithLogger(l *slog.Logger) Option {
	return func(s *Subscriber) { s.logger = l }
}

// WithVisibilityTimeout sets the renewable message lease, in whole seconds.
func WithVisibilityTimeout(d time.Duration) Option {
	return func(s *Subscriber) { s.visibility = d }
}

// WithHandlerTimeout limits processing time, including visibility renewals.
func WithHandlerTimeout(d time.Duration) Option {
	return func(s *Subscriber) { s.handlerTimeout = d }
}

// NewSubscriber returns a Subscriber for queueURL.
func NewSubscriber(client sqsAPI, queueURL string, opts ...Option) *Subscriber {
	s := &Subscriber{client: client, queueURL: queueURL, logger: slog.Default(), visibility: defaultVisibility, handlerTimeout: defaultHandlerTimeout}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run processes one message at a time and retains failed or invalid messages for redelivery and DLQ routing.
func (s *Subscriber) Run(ctx context.Context, h events.Handler) error {
	if s.client == nil || s.logger == nil || s.queueURL == "" || h == nil || s.handlerTimeout <= 0 || s.visibility < time.Second || s.visibility > 12*time.Hour || s.visibility%time.Second != 0 {
		return errors.New("sqs: client, logger, queue, handler, positive handler timeout and whole-second visibility between 1s and 12h are required")
	}
	for ctx.Err() == nil {
		rctx, cancel := context.WithTimeout(ctx, receiveWait+requestTimeout)
		out, err := s.client.ReceiveMessage(rctx, &awssqs.ReceiveMessageInput{
			QueueUrl:              aws.String(s.queueURL),
			MaxNumberOfMessages:   receiveBatchSize,
			WaitTimeSeconds:       int32(receiveWait.Seconds()),
			VisibilityTimeout:     int32(s.visibility.Seconds()),
			MessageAttributeNames: []string{"All"},
		})
		cancel()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil //nolint:nilerr // cancellation is the normal way Run stops
			}
			s.logger.WarnContext(ctx, "sqs receive failed", "error", err)
			sleep(ctx, receiveRetryDelay)
			continue
		}
		for _, m := range out.Messages {
			s.handle(ctx, h, m)
		}
	}
	return nil
}

func (s *Subscriber) handle(ctx context.Context, h events.Handler, m types.Message) {
	var e events.Event
	if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &e); err != nil {
		s.logger.WarnContext(ctx, "undecodable message left for redelivery", "message_id", aws.ToString(m.MessageId), "error", err)
		return
	}
	if err := e.Validate(); err != nil {
		s.logger.WarnContext(ctx, "invalid event left for redelivery", "message_id", aws.ToString(m.MessageId), "error", err)
		return
	}
	hctx, cancel := context.WithTimeout(ctx, s.handlerTimeout)
	defer cancel()
	stop := make(chan struct{})
	renewed := make(chan error, 1)
	go func() {
		err := s.renew(hctx, m, stop)
		if err != nil {
			cancel()
		}
		renewed <- err
	}()
	err := h(hctx, e)
	close(stop)
	renewErr := <-renewed
	if err = errors.Join(err, renewErr, hctx.Err()); err != nil {
		s.logger.WarnContext(ctx, "event handler failed, left for redelivery",
			"message_id", aws.ToString(m.MessageId), "event_id", e.ID, "event_type", e.Type, "error", err)
		return
	}
	s.delete(ctx, m)
}

func (s *Subscriber) renew(ctx context.Context, m types.Message, stop <-chan struct{}) error {
	interval := s.visibility / 3
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return nil
		case <-ticker.C:
			rctx, cancel := context.WithTimeout(ctx, min(requestTimeout, interval))
			_, err := s.client.ChangeMessageVisibility(rctx, &awssqs.ChangeMessageVisibilityInput{
				QueueUrl: aws.String(s.queueURL), ReceiptHandle: m.ReceiptHandle, VisibilityTimeout: int32(s.visibility.Seconds()),
			})
			cancel()
			if err != nil {
				return fmt.Errorf("sqs: renew message visibility: %w", err)
			}
		}
	}
}

// delete uses a separate bounded context after successful processing.
func (s *Subscriber) delete(ctx context.Context, m types.Message) {
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()
	_, err := s.client.DeleteMessage(dctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(s.queueURL),
		ReceiptHandle: m.ReceiptHandle,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "sqs delete failed", "message_id", aws.ToString(m.MessageId), "error", err)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
