// Command api runs the HTTP API, or database migrations with -m migrate.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/bete7512/goauth/pkg/auth"
	"github.com/bete7512/goauth/pkg/types"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/api/deliveryclient"
	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/identity"
	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	"github.com/bete7512/hookbeam/pkg/mailer/backend"
	mailermemory "github.com/bete7512/hookbeam/pkg/mailer/memory"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

func main() {
	mode := flag.String("m", "server", "run mode: server | migrate")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := run(ctx, *mode, flag.Args())
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "api: %v\n", err)
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
		Service: cfg.App.Service,
		Version: cfg.App.Version,
		Env:     cfg.App.Env,
	})
	slog.SetDefault(log)
	log.InfoContext(ctx, "config loaded", "config", cfg)

	if mode == "migrate" {
		return migrate(ctx, cfg, log, args)
	}
	return serve(ctx, cfg, log)
}

// migrate runs the goose command, then goauth's own migrations on `up` and prints them on `status`.
func migrate(ctx context.Context, cfg config.Config, log *slog.Logger, args []string) error {
	pool, err := db.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrations.New(log).Run(ctx, pool, args); err != nil {
		return err
	}
	if len(args) == 0 || (args[0] != "up" && args[0] != "status") {
		return nil
	}

	c, err := cache.New(cache.Config{MaxItems: 1000})
	if err != nil {
		return err
	}
	defer c.Close()
	authn, err := identity.New(identity.Deps{
		Config:  cfg,
		Pool:    pool,
		Mailer:  mailermemory.New(),
		Limiter: ratelimitmemory.New(c),
		Logger:  log,
	})
	if err != nil {
		return err
	}
	if args[0] == "up" {
		log.InfoContext(ctx, "running goauth migrations")
		return identity.Migrate(ctx, authn)
	}
	records, err := authn.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	for _, r := range records {
		log.InfoContext(ctx, "goauth migration", "module", r.ModuleName, "version", r.Version, "name", r.Name, "applied_at", r.AppliedAt, "dirty", r.Dirty)
	}
	return nil
}

// deps is what the HTTP process serves with.
type deps struct {
	auth     *auth.Auth
	handlers handlers.Deps
	monitor  services.MonitorService
}

// build wires the api's dependencies: the database, goauth, the data plane client and the services behind the routes.
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

	appCache, err := cache.New(cache.Config{MaxItems: cfg.Cache.MaxItems})
	if err != nil {
		return fail(err)
	}
	closers = append(closers, appCache.Close)
	limiter := ratelimitmemory.New(appCache)
	mail, err := backend.New(ctx, backend.Config{
		Backend:  cfg.Mailer.Backend,
		SMTPAddr: cfg.Mailer.SMTPAddr,
		From:     cfg.Mailer.From,
		Region:   cfg.Events.Region,
	})
	if err != nil {
		return fail(err)
	}

	authn, err := identity.New(identity.Deps{
		Config:  cfg,
		Pool:    pool,
		Mailer:  mail,
		Limiter: limiter,
		Logger:  log,
	})
	if err != nil {
		return fail(err)
	}
	if err := authn.Initialize(ctx); err != nil {
		return fail(fmt.Errorf("goauth initialize: %w", err))
	}
	closers = append(closers, func() { _ = authn.Close() })
	members, err := identity.NewMemberships(authn.Config().Storage.Organization())
	if err != nil {
		return fail(err)
	}
	users, err := identity.NewUsers(authn.Config().Storage.Core())
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

	apiKeyRepo, err := repos.NewAPIKeyRepo(pool)
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
	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return fail(err)
	}
	tx := db.NewTransactor(pool)

	authz, err := services.NewAuthorizer(services.AuthorizerDeps{Memberships: members, Roles: memberRoleRepo, Logger: log})
	if err != nil {
		return fail(err)
	}
	memberService, err := services.NewMemberService(services.MemberServiceDeps{
		Tx:          tx,
		Memberships: members,
		Roles:       memberRoleRepo,
		Audit:       auditRepo,
		Authz:       authz,
		Users:       users,
		Logger:      log,
	})
	if err != nil {
		return fail(err)
	}
	orgPolicy, err := services.NewOrgPolicy(services.OrgPolicyDeps{Memberships: members, Authz: authz, Logger: log})
	if err != nil {
		return fail(err)
	}
	apiKeys, err := services.NewAPIKeyService(services.APIKeyServiceDeps{
		Tx:     tx,
		Keys:   apiKeyRepo,
		Audit:  auditRepo,
		Authz:  authz,
		Logger: log,
	})
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
	monitor, err := services.NewMonitorService(services.MonitorServiceDeps{Outbox: outboxRepo, Logger: log})
	if err != nil {
		return fail(err)
	}
	return deps{
		auth: authn,
		handlers: handlers.Deps{
			Auth:           authn,
			APIKeys:        apiKeys,
			DB:             pool,
			Delivery:       delivery,
			DeliveryHealth: deliveryclient.Pinger{Conn: conn},
			Members:        memberService,
			OrgPolicy:      orgPolicy,
			Audit:          audit,
			Notifications:  notifications,
			Authz:          authz,
			Limiter:        limiter,
			KeySubmitLimit: cfg.Limits.KeySubmissionsPerMinute,
			AccessCookie:   config.AccessCookie,
			Logger:         log,
		},
		monitor: monitor,
	}, closeAll, nil
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName: "api",
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

	authSpec, err := d.auth.GenerateOpenAPIDocs(types.OpenAPIConfig{
		Title:          "Hookbeam auth",
		Version:        cfg.App.Version,
		UseDefaultAuth: true,
	})
	if err != nil {
		return fmt.Errorf("goauth openapi: %w", err)
	}
	h, err := handlers.New(d.handlers)
	if err != nil {
		return err
	}
	router, err := handlers.NewRouter(h, handlers.RouterOptions{
		Logger:       log,
		MaxBodyBytes: cfg.HTTP.MaxBodyBytes,
		AuthRoutes:   d.auth.Routes(),
		AuthSpec:     authSpec,
		AuthBasePath: config.AuthBasePath,
		// Cookie-authenticated writes are accepted from the console's origin (and the API's own, behind one proxy).
		AllowedOrigins: []string{cfg.Auth.LinkBaseURL, cfg.Auth.APIURL},
	})
	if err != nil {
		return err
	}

	return httpx.NewServer(router, httpx.Options{
		Addr:              cfg.HTTP.Addr,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		ShutdownTimeout:   cfg.HTTP.ShutdownTimeout,
		Logger:            log,
	}).Run(ctx)
}
