// Package sns publishes events to an Amazon SNS topic.
package sns

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/bete7512/hookbeam/pkg/events"
)

const publishBatchSize = 10

// NewClient builds an SNS client for region from the default credential chain; endpoint overrides the AWS URL (LocalStack).
func NewClient(ctx context.Context, region, endpoint string) (*awssns.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("sns: load aws config: %w", err)
	}
	return awssns.NewFromConfig(cfg, func(o *awssns.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

// Publisher sends events to one topic.
type Publisher struct {
	client   *awssns.Client
	topicARN string
}

// NewPublisher returns a Publisher for topicARN.
func NewPublisher(client *awssns.Client, topicARN string) *Publisher {
	return &Publisher{client: client, topicARN: topicARN}
}

// Publish sends evs in batches of ten; the JSON envelope is the message body and the type is a message attribute.
func (p *Publisher) Publish(ctx context.Context, evs []events.Event) error {
	for start := 0; start < len(evs); start += publishBatchSize {
		end := min(start+publishBatchSize, len(evs))
		entries := make([]types.PublishBatchRequestEntry, 0, end-start)
		for i, e := range evs[start:end] {
			body, err := json.Marshal(e)
			if err != nil {
				return fmt.Errorf("sns: marshal event %q: %w", e.Type, err)
			}
			entries = append(entries, types.PublishBatchRequestEntry{
				Id:      aws.String(strconv.Itoa(start + i)),
				Message: aws.String(string(body)),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"type": {DataType: aws.String("String"), StringValue: aws.String(e.Type)},
				},
			})
		}
		out, err := p.client.PublishBatch(ctx, &awssns.PublishBatchInput{
			TopicArn:                   aws.String(p.topicARN),
			PublishBatchRequestEntries: entries,
		})
		if err != nil {
			return fmt.Errorf("sns: publish batch: %w", err)
		}
		if len(out.Failed) > 0 {
			return batchError(out.Failed)
		}
	}
	return nil
}

func batchError(failed []types.BatchResultErrorEntry) error {
	parts := make([]string, 0, len(failed))
	for _, f := range failed {
		parts = append(parts, fmt.Sprintf("entry %s: %s: %s", aws.ToString(f.Id), aws.ToString(f.Code), aws.ToString(f.Message)))
	}
	return fmt.Errorf("sns: publish batch: %d of the entries failed: %s", len(failed), strings.Join(parts, "; "))
}
