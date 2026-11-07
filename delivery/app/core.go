// Package app wires the dependencies shared by the delivery server and worker.
package app

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/transport"
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
	Applications services.ApplicationService
	Endpoints    services.EndpointService
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
	outboxRepo, err := repos.NewOutboxRepo(pool)
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

	applications, err := services.NewApplicationService(services.ApplicationServiceDeps{Applications: applicationRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	endpoints, err := services.NewEndpointService(services.EndpointServiceDeps{
		Tx:            tx,
		Applications:  applicationRepo,
		Endpoints:     endpointRepo,
		Secrets:       secretRepo,
		Subscriptions: subscriptionRepo,
		Cipher:        cipher,
		KeyID:         d.Config.Secrets.KeyID,
		Logger:        d.Logger,
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
		Pool:         pool,
		Applications: applications,
		Endpoints:    endpoints,
		Events:       eventService,
		Publisher:    publisher,
		Subscriber:   subscriber,
	}, nil
}

// Close releases the database pool.
func (c *Core) Close() {
	c.Pool.Close()
}
