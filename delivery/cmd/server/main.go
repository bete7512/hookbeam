// Command server runs the delivery gRPC service, or database migrations with -m migrate.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/rpc"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/grpcx"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

func main() {
	mode := flag.String("m", "server", "run mode: server | migrate")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx, *mode, flag.Args())
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "delivery: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, mode string, args []string) error {
	if mode != "server" && mode != "migrate" {
		return fmt.Errorf("unknown mode %q (want server or migrate)", mode)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(logger.Options{
		Level:   cfg.Log.Level,
		Format:  cfg.Log.Format,
		Service: "delivery",
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)
	log.InfoContext(ctx, "config loaded", "config", cfg)

	if mode == "migrate" {
		pool, err := db.NewPool(ctx, cfg.DB)
		if err != nil {
			return err
		}
		defer pool.Close()
		return migrations.New(log).Run(ctx, pool, args)
	}
	return serve(ctx, cfg, log)
}

// deps is what the gRPC server serves with.
type deps struct {
	srv     *rpc.Server
	monitor services.MonitorService
}

// build wires the server's dependencies: the database, the repos it reads and the services behind the RPCs.
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
	subscriptionRepo, err := repos.NewSubscriptionRepo(pool)
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
	overviewRepo, err := repos.NewOverviewRepo(pool)
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
	limits := services.Limits{
		EndpointsPerApplication: cfg.Limits.EndpointsPerApplication,
		FanoutPerMessage:        cfg.Limits.FanoutPerMessage,
		BacklogPerWorkspace:     cfg.Limits.BacklogPerWorkspace,
		BacklogRetryAfter:       cfg.Limits.BacklogRetryAfter,
	}
	urlPolicy := safehttp.Config{
		Timeout:      cfg.Sender.Timeout,
		AllowedPorts: cfg.Sender.AllowedPorts,
		AllowPrivate: cfg.Sender.AllowPrivate,
	}

	environments, err := services.NewEnvironmentService(services.EnvironmentServiceDeps{
		Environments: environmentRepo,
		Logger:       log,
	})
	if err != nil {
		return fail(err)
	}
	applications, err := services.NewApplicationService(services.ApplicationServiceDeps{
		Tx:           tx,
		Applications: applicationRepo,
		Environments: environmentRepo,
		Endpoints:    endpointRepo,
		Deliveries:   deliveryRepo,
		Audit:        auditRepo,
		Logger:       log,
	})
	if err != nil {
		return fail(err)
	}
	health, err := services.NewHealthService(services.HealthServiceDeps{
		Health: healthRepo,
		Outbox: outboxRepo,
		Logger: log,
	})
	if err != nil {
		return fail(err)
	}
	overview, err := services.NewOverviewService(services.OverviewServiceDeps{
		Environments: environmentRepo,
		Overview:     overviewRepo,
		Logger:       log,
	})
	if err != nil {
		return fail(err)
	}
	limitService, err := services.NewLimitService(services.LimitServiceDeps{
		Endpoints:  endpointRepo,
		Deliveries: deliveryRepo,
		Limits:     limits,
		Retention: models.Retention{
			Payload:           cfg.Payload.Retention,
			IdempotencyWindow: cfg.Payload.IdempotencyWindow,
			Audit:             cfg.Audit.Retention,
		},
		Logger: log,
	})
	if err != nil {
		return fail(err)
	}
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
		KeyID:         cfg.Secrets.KeyID,
		Logger:        log,
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
	audit, err := services.NewAuditService(services.AuditServiceDeps{
		Audit:     auditRepo,
		Retention: cfg.Audit.Retention,
		BatchSize: cfg.Payload.ExpiryBatchSize,
		Logger:    log,
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
	srv, err := rpc.New(rpc.Deps{
		Audit:        audit,
		Environments: environments,
		Applications: applications,
		Endpoints:    endpoints,
		Health:       health,
		Limits:       limitService,
		Overview:     overview,
		Messages:     messages,
		Logger:       log,
	})
	if err != nil {
		return fail(err)
	}
	return deps{srv: srv, monitor: monitor}, closeAll, nil
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName: "delivery",
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
	if cfg.Internal.Addr != "" {
		go func() { _ = httpx.NewInternalServer(cfg.Internal.Addr, cfg.Internal.Token, log).Run(ctx) }()
	}

	creds, err := cfg.TLS.ServerCredentials()
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(grpc.Creds(creds), grpcx.TracingServer(), grpcx.ServerInterceptors(log))
	deliveryv1.RegisterDeliveryServiceServer(grpcServer, d.srv)
	health := grpcx.RegisterHealth(grpcServer)

	lis, err := net.Listen("tcp", cfg.GRPC.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.GRPC.Addr, err)
	}
	log.InfoContext(ctx, "delivery server listening", "addr", lis.Addr().String(), "tls_insecure", cfg.TLS.Insecure)

	errCh := make(chan error, 1)
	go func() { errCh <- grpcServer.Serve(lis) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	health.Shutdown()
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(cfg.GRPC.ShutdownTimeout):
		grpcServer.Stop()
	}
	if err := <-errCh; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}
	log.InfoContext(context.Background(), "delivery server stopped")
	return nil
}
