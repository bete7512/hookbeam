// Package transport builds the publisher and subscriber a service's configuration asks for.
package transport

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bete7512/hookbeam/pkg/config"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/memory"
	"github.com/bete7512/hookbeam/pkg/events/sns"
	"github.com/bete7512/hookbeam/pkg/events/sqs"
)

// New returns the transport for cfg.Backend: an in-process bus for "memory", SNS and SQS for "aws".
func New(ctx context.Context, cfg config.Events, log *slog.Logger) (events.Publisher, events.Subscriber, error) {
	switch cfg.Backend {
	case "memory":
		bus := memory.New(1024)
		return bus, bus, nil
	case "aws":
		snsClient, err := sns.NewClient(ctx, cfg.Region, cfg.Endpoint)
		if err != nil {
			return nil, nil, err
		}
		sqsClient, err := sqs.NewClient(ctx, cfg.Region, cfg.Endpoint)
		if err != nil {
			return nil, nil, err
		}
		return sns.NewPublisher(snsClient, cfg.TopicARN), sqs.NewSubscriber(sqsClient, cfg.QueueURL, sqs.WithLogger(log), sqs.WithHandlerTimeout(cfg.HandlerTimeout)), nil
	default:
		return nil, nil, fmt.Errorf("transport: unknown events backend %q", cfg.Backend)
	}
}
