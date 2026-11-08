// Package app wires the dependencies shared by the delivery server and worker.
package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/sender"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/transport"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
)

// Deps are what NewCore needs.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// Core holds the resources and services shared by the server and the worker.
type Core struct {
	Pool         *pgxpool.Pool
	Environments services.EnvironmentService
	Applications services.ApplicationService
	Endpoints    services.EndpointService
	Messages     services.MessageService
	Dispatch     services.DispatchService
	Health       services.HealthService
	Limits       services.LimitService
	Audit        services.AuditService
	Events       services.EventService
	Publisher    events.Publisher
	Subscriber   events.Subscriber
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

	applicationRepo, err := repos.NewApplicationRepo(pool)
	if err != nil {
		return nil, err
	}
	auditRepo, err := repos.NewAuditRepo(pool)
	if err != nil {
		return nil, err
	}
	environmentRepo, err := repos.NewEnvironmentRepo(pool)
	if err != nil {
		return nil, err
	}
	endpointRepo, err := repos.NewEndpointRepo(pool)
	if err != nil {
		return nil, err
	}
	secretRepo, err := repos.NewEndpointSecretRepo(pool)
	if err != nil {
		return nil, err
	}
	subscriptionRepo, err := repos.NewSubscriptionRepo(pool)
	if err != nil {
		return nil, err
	}
	messageRepo, err := repos.NewMessageRepo(pool)
	if err != nil {
		return nil, err
	}
	deliveryRepo, err := repos.NewDeliveryRepo(pool)
	if err != nil {
		return nil, err
	}
	attemptRepo, err := repos.NewAttemptRepo(pool)
	if err != nil {
		return nil, err
	}
	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return nil, err
	}
	healthRepo, err := repos.NewHealthRepo(pool)
	if err != nil {
		return nil, err
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return nil, err
	}
	tx := db.NewTransactor(pool)

	cipher, err := secrets.New(d.Config.Secrets.Key)
	if err != nil {
		return nil, err
	}
	publisher, subscriber, err := transport.New(ctx, d.Config.Events, d.Logger)
	if err != nil {
		return nil, err
	}

	environments, err := services.NewEnvironmentService(services.EnvironmentServiceDeps{Environments: environmentRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	applications, err := services.NewApplicationService(services.ApplicationServiceDeps{Tx: tx, Applications: applicationRepo, Environments: environmentRepo, Audit: auditRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	health, err := services.NewHealthService(services.HealthServiceDeps{Health: healthRepo, Outbox: outboxRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	limits := services.Limits{EndpointsPerApplication: d.Config.Limits.EndpointsPerApplication, FanoutPerMessage: d.Config.Limits.FanoutPerMessage, BacklogPerWorkspace: d.Config.Limits.BacklogPerWorkspace, BacklogRetryAfter: d.Config.Limits.BacklogRetryAfter}
	limitService, err := services.NewLimitService(services.LimitServiceDeps{Endpoints: endpointRepo, Deliveries: deliveryRepo, Limits: limits, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	urlPolicy := safehttp.Config{Timeout: d.Config.Sender.Timeout, AllowedPorts: d.Config.Sender.AllowedPorts, AllowPrivate: d.Config.Sender.AllowPrivate}
	endpoints, err := services.NewEndpointService(services.EndpointServiceDeps{
		Tx:            tx,
		Applications:  applicationRepo,
		Endpoints:     endpointRepo,
		Deliveries:    deliveryRepo,
		Audit:         auditRepo,
		URL:           urlPolicy,
		Secrets:       secretRepo,
		Subscriptions: subscriptionRepo,
		Health:        health,
		Limits:        limits,
		Cipher:        cipher,
		KeyID:         d.Config.Secrets.KeyID,
		Logger:        d.Logger,
	})
	if err != nil {
		return nil, err
	}
	messages, err := services.NewMessageService(services.MessageServiceDeps{
		Tx:                tx,
		Applications:      applicationRepo,
		Environments:      environmentRepo,
		Endpoints:         endpointRepo,
		Messages:          messageRepo,
		Deliveries:        deliveryRepo,
		Attempts:          attemptRepo,
		Outbox:            outboxRepo,
		Audit:             auditRepo,
		Policy:            services.DefaultPolicy,
		Limits:            limits,
		PayloadRetention:  d.Config.Payload.Retention,
		IdempotencyWindow: d.Config.Payload.IdempotencyWindow,
		MaxPayloadBytes:   d.Config.Payload.MaxBytes,
		ExpiryBatchSize:   d.Config.Payload.ExpiryBatchSize,
		Logger:            d.Logger,
	})
	if err != nil {
		return nil, err
	}
	httpClient := urlPolicy.NewClient()
	dispatch, err := services.NewDispatchService(services.DispatchServiceDeps{
		Tx:                  tx,
		Deliveries:          deliveryRepo,
		Attempts:            attemptRepo,
		Messages:            messageRepo,
		Endpoints:           endpointRepo,
		Secrets:             secretRepo,
		Outbox:              outboxRepo,
		Health:              health,
		Cipher:              cipher,
		Sender:              sender.New(httpClient, sender.Config{MaxResponseBytes: d.Config.Sender.MaxResponseBytes, MaxPreviewBytes: d.Config.Sender.MaxPreviewBytes}),
		Policy:              services.DefaultPolicy,
		LeaseTTL:            d.Config.Claims.LeaseTTL,
		BatchSize:           d.Config.Claims.BatchSize,
		EndpointConcurrency: d.Config.Claims.EndpointConcurrency,
		Concurrency:         d.Config.Claims.Concurrency,
		Logger:              d.Logger,
	})
	if err != nil {
		return nil, err
	}
	auditService, err := services.NewAuditService(services.AuditServiceDeps{Audit: auditRepo, Retention: d.Config.Audit.Retention, BatchSize: d.Config.Payload.ExpiryBatchSize, Logger: d.Logger})
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
		Pool:         pool,
		Environments: environments,
		Applications: applications,
		Endpoints:    endpoints,
		Messages:     messages,
		Dispatch:     dispatch,
		Health:       health,
		Limits:       limitService,
		Audit:        auditService,
		Events:       eventService,
		Publisher:    publisher,
		Subscriber:   subscriber,
	}, nil
}

// Close releases the database pool.
func (c *Core) Close() {
	c.Pool.Close()
}
