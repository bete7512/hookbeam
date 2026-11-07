// Package ses delivers mail through Amazon SES v2.
package ses

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"github.com/bete7512/hookbeam/pkg/mailer"
)

// NewClient builds an SES client from the default credential chain in region.
func NewClient(ctx context.Context, region string) (*sesv2.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return sesv2.NewFromConfig(cfg), nil
}

// Mailer sends simple messages from one verified address.
type Mailer struct {
	client *sesv2.Client
	from   string
}

// New returns a Mailer sending as from through client.
func New(client *sesv2.Client, from string) *Mailer {
	return &Mailer{client: client, from: from}
}

// Send delivers msg with SendEmail.
func (m *Mailer) Send(ctx context.Context, msg mailer.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	body := &types.Body{}
	if msg.Text != "" {
		body.Text = content(msg.Text)
	}
	if msg.HTML != "" {
		body.Html = content(msg.HTML)
	}
	_, err := m.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(m.from),
		Destination:      &types.Destination{ToAddresses: []string{msg.To}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: content(msg.Subject),
			Body:    body,
		}},
	})
	return err
}

func content(s string) *types.Content {
	return &types.Content{Data: aws.String(s), Charset: aws.String("UTF-8")}
}
