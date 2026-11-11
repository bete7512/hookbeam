// Command worker relays the outbox to the queue and runs the event handlers.
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

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/api/deliveryclient"
	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/identity"
	"github.com/bete7512/hookbeam/api/jobs"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/transport"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	"github.com/bete7512/hookbeam/pkg/mailer/backend"
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
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

// deps is what the worker's loops and handlers run over.
type deps struct {
	events         services.EventService
	audit          services.AuditService
	endpointHealth services.EndpointHealthService
	notifications  services.NotificationService
	monitor        services.MonitorService
	metrics        *telemetry.Metrics
	subscriber     events.Subscriber
}

// build wires the worker's dependencies: the database, the queue transport, goauth's storage (read only, the
// worker serves no identity routes), the data plane client and the services the handlers call.
func build(ctx context.Context, cfg config.Config, log *slog.Logger) (deps, func(), error) {
	pool, err := db.NewPoolWithRetries(ctx, log, cfg.DB)
	if err != nil {
		return deps{}, nil, err
	}
	closers := []func(){pool.Close}
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	fail := func(err error) (deps, func(), error) { closeAll(); return deps{}, nil, err }

	metrics, err := telemetry.NewMetrics()
	if err != nil {
		return fail(err)
	}
	publisher, subscriber, err := transport.New(ctx, cfg.Events, log)
	if err != nil {
		return fail(err)
	}
	mail, err := backend.New(ctx, backend.Config{
		Backend:  cfg.Mailer.Backend,
		SMTPAddr: cfg.Mailer.SMTPAddr,
		From:     cfg.Mailer.From,
		Region:   cfg.Events.Region,
	})
	if err != nil {
		return fail(err)
	}
	store, err := identity.OpenStorage(pool)
	if err != nil {
		return fail(err)
	}
	members, err := identity.NewMemberships(store.Organization())
	if err != nil {
		return fail(err)
	}
	users, err := identity.NewUsers(store.Core())
	if err != nil {
		return fail(err)
	}
	conn, err := deliveryclient.Dial(cfg.Delivery)
	if err != nil {
		return fail(err)
	}
	closers = append(closers, func() { _ = conn.Close() })
	dataPlane, err := deliveryclient.New(deliveryv1.NewDeliveryServiceClient(conn), cfg.Delivery.Timeout)
	if err != nil {
		return fail(err)
	}

	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return fail(err)
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return fail(err)
	}
	healthRepo, err := repos.NewEndpointHealthRepo(pool)
	if err != nil {
		return fail(err)
	}
	memberRoleRepo, err := repos.NewMemberRoleRepo(pool)
	if err != nil {
		return fail(err)
	}
	auditRepo, err := repos.NewAuditRepo(pool)
	if err != nil {
		return fail(err)
	}
	notificationRepo, err := repos.NewNotificationRepo(pool)
	if err != nil {
		return fail(err)
	}
	preferenceRepo, err := repos.NewNotificationPreferenceRepo(pool)
	if err != nil {
		return fail(err)
	}
	tx := db.NewTransactor(pool)

	authz, err := services.NewAuthorizer(services.AuthorizerDeps{Memberships: members, Roles: memberRoleRepo, Logger: log})
	if err != nil {
		return fail(err)
	}
	delivery, err := services.NewDeliveryService(services.DeliveryServiceDeps{
		DataPlane: dataPlane,
		Authz:     authz,
		Logger:    log,
	})
	if err != nil {
		return fail(err)
	}
	endpointHealth, err := services.NewEndpointHealthService(services.EndpointHealthServiceDeps{
		Health: healthRepo,
		Logger: log,
	})
	if err != nil {
		return fail(err)
	}
	notifications, err := services.NewNotificationService(services.NotificationServiceDeps{
		Tx:            tx,
		Notifications: notificationRepo,
		Preferences:   preferenceRepo,
		Outbox:        outboxRepo,
		Memberships:   members,
		Users:         users,
		Delivery:      delivery,
		Mailer:        mail,
		LinkBaseURL:   cfg.Auth.LinkBaseURL,
		Logger:        log,
	})
	if err != nil {
		return fail(err)
	}
	audit, err := services.NewAuditService(services.AuditServiceDeps{
		Audit:     auditRepo,
		Delivery:  delivery,
		Authz:     authz,
		Retention: cfg.Audit.Retention,
		BatchSize: cfg.Outbox.BatchSize,
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
	monitor, err := services.NewMonitorService(services.MonitorServiceDeps{Outbox: outboxRepo, Logger: log})
	if err != nil {
		return fail(err)
	}
	return deps{
		events:         eventService,
		audit:          audit,
		endpointHealth: endpointHealth,
		notifications:  notifications,
		monitor:        monitor,
		metrics:        metrics,
		subscriber:     subscriber,
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
		Service: "worker",
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)

	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName: "worker",
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

	// Account emails are goauth's; the data plane's delivery state events feed the endpoint health projection.
	handlers := map[string]jobs.Handler{}
	for _, t := range apievents.DeliveryStateTypes {
		handlers[t] = jobs.NewDeliveryState(d.endpointHealth)
	}
	handlers[apievents.EndpointHealthChanged] = jobs.NewEndpointHealthChanged(d.notifications)
	handlers[apievents.NotificationEmailRequested] = jobs.NewNotificationEmail(d.notifications)
	j, err := jobs.New(jobs.Deps{
		Events:         d.events,
		Logger:         log,
		HandlerTimeout: cfg.Events.HandlerTimeout,
		Handlers:       handlers,
		Metrics:        d.metrics,
	})
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "worker started", "events_backend", cfg.Events.Backend, "mailer_backend", cfg.Mailer.Backend)
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
	g.Go(func() error { return d.subscriber.Run(gctx, j.Handle) })
	err = g.Wait()
	log.InfoContext(context.Background(), "worker stopped")
	return err
}
