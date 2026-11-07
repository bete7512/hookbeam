package config

import (
	"errors"
	"time"
)

// Events configures the event transport. AWS credentials come from the standard AWS_* variables.
type Events struct {
	HandlerTimeout time.Duration `env:"HANDLER_TIMEOUT" envDefault:"2m"`
	Backend        string        `env:"BACKEND" envDefault:"memory"` // aws | memory
	Region         string        `env:"AWS_REGION" envDefault:"us-east-1"`
	Endpoint       string        `env:"AWS_ENDPOINT_URL"` // LocalStack; empty in AWS
	TopicARN       string        `env:"TOPIC_ARN"`
	QueueURL       string        `env:"QUEUE_URL"`
}

// Validate checks the backend and its settings; prefix names the variables in messages, e.g. "EVENTS_".
func (e Events) Validate(prefix string, production bool) error {
	var errs []error
	switch e.Backend {
	case "memory":
	case "aws":
		if e.TopicARN == "" || e.QueueURL == "" {
			errs = append(errs, errors.New(prefix+"TOPIC_ARN and "+prefix+"QUEUE_URL are required when "+prefix+"BACKEND=aws"))
		}
	default:
		errs = append(errs, errors.New(prefix+"BACKEND must be aws or memory"))
	}
	if e.HandlerTimeout <= 0 {
		errs = append(errs, errors.New(prefix+"HANDLER_TIMEOUT must be positive"))
	}
	if production && (e.Backend != "aws" || e.Endpoint != "") {
		errs = append(errs, errors.New("staging/prod require AWS events without a local endpoint override"))
	}
	return errors.Join(errs...)
}

// Outbox configures the relay and cleanup that run in the worker.
type Outbox struct {
	PublishTimeout time.Duration `env:"PUBLISH_TIMEOUT" envDefault:"30s"`
	PollInterval   time.Duration `env:"POLL_INTERVAL" envDefault:"500ms"`
	BatchSize      int           `env:"BATCH_SIZE" envDefault:"100"`
	Retention      time.Duration `env:"RETENTION" envDefault:"168h"`
}

// Validate checks that every setting is positive; prefix names the variables in messages, e.g. "OUTBOX_".
func (o Outbox) Validate(prefix string) error {
	if o.PollInterval <= 0 || o.BatchSize <= 0 || o.Retention <= 0 || o.PublishTimeout <= 0 {
		return errors.New(prefix + "POLL_INTERVAL, " + prefix + "BATCH_SIZE, " + prefix + "RETENTION and " + prefix + "PUBLISH_TIMEOUT must be positive")
	}
	return nil
}
