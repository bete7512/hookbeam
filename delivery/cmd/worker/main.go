// Command worker attempts due deliveries and relays the delivery outbox to the queue.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/sender"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/transport"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/telemetry"
	"github.com/bete7512/hookbeam/pkg/worker"
)

const (
	cleanInterval = time.Hour
	cleanTimeout  = time.Minute
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "delivery-worker: %v\n", err)
		os.Exit(1)
	}
}

// deps is what the worker's loops run over.
type deps struct {
	messages   services.MessageService
	dispatch   services.DispatchService
	audit      services.AuditService
	events     services.EventService
	monitor    services.MonitorService
	subscriber events.Subscriber
}

// build wires the worker's dependencies: the database, the queue transport, the sender and the services its loops call.
func build(ctx context.Context, cfg config.Config, log *slog.Logger) (deps, func(), error) {
	pool, err := db.NewPoolWithRetries(ctx, log, cfg.DB)
	if err != nil {
		return deps{}, nil, err
	}
	closeAll := pool.Close
	fail := func(err error) (deps, func(), error) { closeAll(); return deps{}, nil, err }

	applicationRepo, err := repos.NewApplicationRepo(pool)
	if err != nil {
		return fail(err)
	}
	auditRepo, err := repos.NewAuditRepo(pool)
	if err != nil {
		return fail(err)
	}
	environmentRepo, err := repos.NewEnvironmentRepo(pool)
	if err != nil {
		return fail(err)
	}
	endpointRepo, err := repos.NewEndpointRepo(pool)
	if err != nil {
		return fail(err)
	}
	secretRepo, err := repos.NewEndpointSecretRepo(pool)
	if err != nil {
		return fail(err)
	}
	messageRepo, err := repos.NewMessageRepo(pool)
	if err != nil {
		return fail(err)
	}
	deliveryRepo, err := repos.NewDeliveryRepo(pool)
	if err != nil {
		return fail(err)
	}
	attemptRepo, err := repos.NewAttemptRepo(pool)
	if err != nil {
		return fail(err)
	}
	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return fail(err)
	}
	healthRepo, err := repos.NewHealthRepo(pool)
	if err != nil {
		return fail(err)
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return fail(err)
	}
	tx := db.NewTransactor(pool)
	metrics, err := telemetry.NewMetrics()
	if err != nil {
		return fail(err)
	}
	cipher, err := secrets.New(cfg.Secrets.Key)
	if err != nil {
		return fail(err)
	}
	publisher, subscriber, err := transport.New(ctx, cfg.Events, log)
	if err != nil {
		return fail(err)
	}
	limits := services.Limits{
		EndpointsPerApplication: cfg.Limits.EndpointsPerApplication,
		FanoutPerMessage:        cfg.Limits.FanoutPerMessage,
		BacklogPerWorkspace:     cfg.Limits.BacklogPerWorkspace,
		BacklogRetryAfter:       cfg.Limits.BacklogRetryAfter,
	}
	httpClient := safehttp.Config{
		Timeout:      cfg.Sender.Timeout,
		AllowedPorts: cfg.Sender.AllowedPorts,
		AllowPrivate: cfg.Sender.AllowPrivate,
	}.NewClient()

	health, err := services.NewHealthService(services.HealthServiceDeps{
		Health: healthRepo,
		Outbox: outboxRepo,
		Logger: log,
	})
	if err != nil {
		return fail(err)
	}
	messages, err := services.NewMessageService(services.MessageServiceDeps{
		Metrics:           metrics,
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
		PayloadRetention:  cfg.Payload.Retention,
		IdempotencyWindow: cfg.Payload.IdempotencyWindow,
		MaxPayloadBytes:   cfg.Payload.MaxBytes,
		ExpiryBatchSize:   cfg.Payload.ExpiryBatchSize,
		Logger:            log,
	})
	if err != nil {
		return fail(err)
	}
	dispatch, err := services.NewDispatchService(services.DispatchServiceDeps{
		Metrics:    metrics,
		Tx:         tx,
		Deliveries: deliveryRepo,
		Attempts:   attemptRepo,
		Messages:   messageRepo,
		Endpoints:  endpointRepo,
		Secrets:    secretRepo,
		Outbox:     outboxRepo,
		Health:     health,
		Cipher:     cipher,
		Policy:     services.DefaultPolicy,
		Sender: sender.New(httpClient, sender.Config{
			MaxResponseBytes: cfg.Sender.MaxResponseBytes,
			MaxPreviewBytes:  cfg.Sender.MaxPreviewBytes,
		}),
		LeaseTTL:            cfg.Claims.LeaseTTL,
		BatchSize:           cfg.Claims.BatchSize,
		EndpointConcurrency: cfg.Claims.EndpointConcurrency,
		Concurrency:         cfg.Claims.Concurrency,
		Logger:              log,
	})
	if err != nil {
		return fail(err)
	}
	audit, err := services.NewAuditService(services.AuditServiceDeps{
		Audit:     auditRepo,
		Retention: cfg.Audit.Retention,
		BatchSize: cfg.Payload.ExpiryBatchSize,
		Logger:    log,
	})
	if err != nil {
		return fail(err)
	}
	eventService, err := services.NewEventService(services.EventServiceDeps{
		Metrics:        metrics,
		Tx:             tx,
		Outbox:         outboxRepo,
		Processed:      processedRepo,
		Publisher:      publisher,
		BatchSize:      cfg.Outbox.BatchSize,
		Retention:      cfg.Outbox.Retention,
		PublishTimeout: cfg.Outbox.PublishTimeout,
		Logger:         log,
	})
	if err != nil {
		return fail(err)
	}
	monitor, err := services.NewMonitorService(services.MonitorServiceDeps{
		Outbox:     outboxRepo,
		Deliveries: deliveryRepo,
		Logger:     log,
	})
	if err != nil {
		return fail(err)
	}
	return deps{
		messages:   messages,
		dispatch:   dispatch,
		audit:      audit,
		events:     eventService,
		monitor:    monitor,
		subscriber: subscriber,
	}, closeAll, nil
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(logger.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: "delivery-worker",
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)

	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName: "delivery-worker",
		Version:     cfg.App.Version,
		Env:         cfg.App.Env,
		Endpoint:    cfg.Telemetry.Endpoint,
		Interval:    cfg.Telemetry.Interval,
	})
	if err != nil {
		return err
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()
	d, closeAll, err := build(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer closeAll()
	if err := d.monitor.RegisterGauges(); err != nil {
		return err
	}

	log.InfoContext(ctx, "delivery worker started", "events_backend", cfg.Events.Backend, "claim_interval", cfg.Claims.Interval)
	wake := make(chan struct{}, 1)
	g, gctx := errgroup.WithContext(ctx)
	if cfg.Internal.Addr != "" {
		g.Go(func() error { return httpx.NewInternalServer(cfg.Internal.Addr, cfg.Internal.Token, log).Run(gctx) })
	}
	g.Go(func() error {
		return worker.Every(gctx, worker.Loop{
			Name:     "publish events",
			Interval: cfg.Outbox.PollInterval,
			Log:      log,
		}, func(ctx context.Context) error {
			_, err := d.events.Publish(ctx)
			return err
		})
	})
	g.Go(func() error {
		return worker.Every(gctx, worker.Loop{
			Name:     "clean outbox",
			Interval: cleanInterval,
			Timeout:  cleanTimeout,
			Log:      log,
		}, func(ctx context.Context) error {
			_, err := d.events.CleanOutbox(ctx)
			return err
		})
	})
	g.Go(func() error {
		// Claim and attempt due deliveries now, on every wake-up and after every interval, draining until a batch comes back short.
		return worker.Every(gctx, worker.Loop{
			Name:     "dispatch",
			Interval: cfg.Claims.Interval,
			Wake:     wake,
			Log:      log,
		}, func(ctx context.Context) error {
			return drain(ctx, d.dispatch.Dispatch)
		})
	})
	g.Go(func() error {
		// Recover deliveries whose lease expired without an outcome, draining until a batch comes back short.
		return worker.Every(gctx, worker.Loop{
			Name:     "sweep leases",
			Interval: cfg.Claims.SweepInterval,
			Log:      log,
		}, func(ctx context.Context) error {
			return drain(ctx, d.dispatch.Sweep)
		})
	})
	g.Go(func() error {
		// Empty payloads and clear idempotency keys past their retention.
		return worker.Every(gctx, worker.Loop{
			Name:     "expire retention",
			Interval: cfg.Payload.ExpiryInterval,
			Timeout:  cleanTimeout,
			Log:      log,
		}, func(ctx context.Context) error {
			payloads, err := d.messages.ExpirePayloads(ctx)
			if err != nil {
				return err
			}
			keys, err := d.messages.ExpireIdempotencyKeys(ctx)
			if err != nil {
				return err
			}
			if payloads > 0 || keys > 0 {
				log.InfoContext(ctx, "retention applied", "payloads_emptied", payloads, "keys_cleared", keys)
			}
			return nil
		})
	})
	g.Go(func() error {
		return worker.Every(gctx, worker.Loop{
			Name:     "prune audit",
			Interval: cleanInterval,
			Timeout:  cleanTimeout,
			Log:      log,
		}, func(ctx context.Context) error {
			n, err := d.audit.Prune(ctx)
			if n > 0 {
				log.InfoContext(ctx, "audit pruned", "count", n)
			}
			return err
		})
	})
	g.Go(func() error {
		// Every event on the queue is a wake-up: claim whatever is due, so duplicates and late deliveries are harmless.
		return d.subscriber.Run(gctx, func(ctx context.Context, e events.Event) error {
			// A wake-up carries no work; its span links to the transaction that queued it, so the hop is visible.
			_, span := telemetry.StartLinked(ctx, "wakeup "+e.Type, e.TraceParent)
			span.End()
			select {
			case wake <- struct{}{}:
			default:
			}
			return nil
		})
	})
	err = g.Wait()
	log.InfoContext(context.Background(), "waiting for attempts in progress", "in_flight_bound", cfg.Claims.Concurrency)
	d.dispatch.Drain()
	log.InfoContext(context.Background(), "delivery worker stopped")
	return err
}

// drain calls step until it reports an empty batch, an error or a done context.
func drain(ctx context.Context, step func(context.Context) (int, error)) error {
	for ctx.Err() == nil {
		n, err := step(ctx)
		if err != nil || n == 0 {
			return err
		}
	}
	return nil
}
