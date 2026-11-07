// Package app wires the dependencies shared by every binary.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/scaffold/apps/api/config"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/apps/api/services"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/cache"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/events"
	eventsmemory "github.com/bete7512/scaffold/pkg/events/memory"
	"github.com/bete7512/scaffold/pkg/events/sns"
	"github.com/bete7512/scaffold/pkg/events/sqs"
	"github.com/bete7512/scaffold/pkg/mailer"
	mailermemory "github.com/bete7512/scaffold/pkg/mailer/memory"
	"github.com/bete7512/scaffold/pkg/mailer/ses"
	"github.com/bete7512/scaffold/pkg/mailer/smtp"
	"github.com/bete7512/scaffold/pkg/ratelimit"
	ratelimitmemory "github.com/bete7512/scaffold/pkg/ratelimit/memory"
)

// Deps are what NewCore needs.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// Core holds the resources and services shared by the api and the worker.
type Core struct {
	Pool        *pgxpool.Pool
	Users       services.UserService
	Auth        services.AuthService
	Events      services.EventService
	Cache       *cache.Cache
	RateLimiter ratelimit.Limiter
	Publisher   events.Publisher
	Subscriber  events.Subscriber
	Mailer      mailer.Mailer
}

// NewCore opens the database pool and builds repos and services.
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

	userRepo, err := repos.NewUserRepo(pool)
	if err != nil {
		return nil, err
	}
	sessionRepo, err := repos.NewSessionRepo(pool)
	if err != nil {
		return nil, err
	}
	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return nil, err
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return nil, err
	}
	authTokenRepo, err := repos.NewAuthTokenRepo(pool)
	if err != nil {
		return nil, err
	}
	identityRepo, err := repos.NewUserIdentityRepo(pool)
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

	params := auth.DefaultArgon2idParams()
	params.Memory = d.Config.Auth.Argon2MemoryKiB
	params.Iterations = d.Config.Auth.Argon2Iterations
	params.Parallelism = d.Config.Auth.Argon2Parallelism
	hasher := auth.NewArgon2id(params)

	tokens, err := auth.NewTokens(d.Config.Auth.SigningKeySeed(), d.Config.Auth.Issuer, d.Config.Auth.AccessTokenTTL)
	if err != nil {
		return nil, err
	}

	publisher, subscriber, err := newEvents(ctx, d.Config.Events, d.Logger)
	if err != nil {
		return nil, err
	}
	mail, err := newMailer(ctx, d.Config.Mailer, d.Config.Events.Region)
	if err != nil {
		return nil, err
	}

	users, err := services.NewUserService(services.UserServiceDeps{
		Users:    userRepo,
		Sessions: sessionRepo,
		Hasher:   hasher,
		Tx:       tx,
		Outbox:   outboxRepo,
		Logger:   d.Logger,
	})
	if err != nil {
		return nil, err
	}
	authService, err := services.NewAuthService(services.AuthServiceDeps{
		Users:             userRepo,
		Sessions:          sessionRepo,
		AuthTokens:        authTokenRepo,
		Identities:        identityRepo,
		Outbox:            outboxRepo,
		Tx:                tx,
		Hasher:            hasher,
		Tokens:            tokens,
		Limiter:           limiter,
		LoginLimit:        d.Config.RateLimit.LoginLimit,
		LoginWindow:       d.Config.RateLimit.LoginWindow,
		RefreshTokenTTL:   d.Config.Auth.RefreshTokenTTL,
		ActionTokenTTL:    d.Config.Auth.ActionTokenTTL,
		ActionTokenKey:    d.Config.Auth.ActionKey(),
		RecipientCooldown: d.Config.Auth.RecipientCooldown,
		Logger:            d.Logger,
	})
	if err != nil {
		return nil, err
	}

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
		Users:       users,
		Auth:        authService,
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

// Close releases the cache and the database pool.
func (c *Core) Close() {
	c.Cache.Close()
	c.Pool.Close()
}
