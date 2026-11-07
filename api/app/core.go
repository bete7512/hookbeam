// Package app wires the dependencies shared by every binary.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bete7512/goauth/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	eventsmemory "github.com/bete7512/hookbeam/pkg/events/memory"
	"github.com/bete7512/hookbeam/pkg/events/sns"
	"github.com/bete7512/hookbeam/pkg/events/sqs"
	"github.com/bete7512/hookbeam/pkg/mailer"
	mailermemory "github.com/bete7512/hookbeam/pkg/mailer/memory"
	"github.com/bete7512/hookbeam/pkg/mailer/ses"
	"github.com/bete7512/hookbeam/pkg/mailer/smtp"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
)

// Deps are what NewCore needs.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// Core holds the resources and services shared by the api and the worker.
type Core struct {
	Pool        *pgxpool.Pool
	Auth        *auth.Auth
	Events      services.EventService
	Cache       *cache.Cache
	RateLimiter ratelimit.Limiter
	Publisher   events.Publisher
	Subscriber  events.Subscriber
	Mailer      mailer.Mailer
}

// NewCore opens the database pool, builds repos and services, and initializes goauth.
func NewCore(ctx context.Context, d Deps) (_ *Core, err error) {
	pool, err := db.NewPoolWithRetries(ctx, d.Logger, d.Config.DB)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			pool.Close()
		}
	}()

	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return nil, err
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return nil, err
	}
	tx := db.NewTransactor(pool)
	appCache, err := cache.New(cache.Config{MaxItems: d.Config.Cache.MaxItems})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			appCache.Close()
		}
	}()
	limiter := ratelimitmemory.New(appCache)

	publisher, subscriber, err := newEvents(ctx, d.Config.Events, d.Logger)
	if err != nil {
		return nil, err
	}
	mail, err := newMailer(ctx, d.Config.Mailer, d.Config.Events.Region)
	if err != nil {
		return nil, err
	}

	authn, err := NewAuth(AuthDeps{Config: d.Config, Pool: pool, Mailer: mail, Limiter: limiter, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	if err := authn.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("app: goauth initialize: %w", err)
	}
	defer func() {
		if err != nil {
			_ = authn.Close()
		}
	}()

	eventService, err := services.NewEventService(services.EventServiceDeps{
		Tx:             tx,
		Outbox:         outboxRepo,
		Processed:      processedRepo,
		Publisher:      publisher,
		BatchSize:      d.Config.Outbox.BatchSize,
		Retention:      d.Config.Outbox.Retention,
		PublishTimeout: d.Config.Outbox.PublishTimeout,
		Logger:         d.Logger,
	})
	if err != nil {
		return nil, err
	}

	return &Core{
		Pool:        pool,
		Auth:        authn,
		Events:      eventService,
		Cache:       appCache,
		RateLimiter: limiter,
		Publisher:   publisher,
		Subscriber:  subscriber,
		Mailer:      mail,
	}, nil
}

func newEvents(ctx context.Context, cfg config.Events, log *slog.Logger) (events.Publisher, events.Subscriber, error) {
	switch cfg.Backend {
	case "memory":
		bus := eventsmemory.New(1024)
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
		return nil, nil, fmt.Errorf("app: unknown events backend %q", cfg.Backend)
	}
}

func newMailer(ctx context.Context, cfg config.Mailer, region string) (mailer.Mailer, error) {
	switch cfg.Backend {
	case "memory":
		return mailermemory.New(), nil
	case "smtp":
		return smtp.New(smtp.Config{Addr: cfg.SMTPAddr, From: cfg.From}), nil
	case "ses":
		client, err := ses.NewClient(ctx, region)
		if err != nil {
			return nil, err
		}
		return ses.New(client, cfg.From), nil
	default:
		return nil, fmt.Errorf("app: unknown mailer backend %q", cfg.Backend)
	}
}

// Close stops goauth and releases the cache and the database pool.
func (c *Core) Close() {
	_ = c.Auth.Close()
	c.Cache.Close()
	c.Pool.Close()
}
