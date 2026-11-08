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

	"github.com/bete7512/goauth/pkg/types"

	"github.com/bete7512/hookbeam/api/app"
	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/api/handlers"
	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/httpx"
	"github.com/bete7512/hookbeam/pkg/logger"
	mailermemory "github.com/bete7512/hookbeam/pkg/mailer/memory"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
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
	authn, err := app.NewAuth(app.AuthDeps{Config: cfg, Pool: pool, Mailer: mailermemory.New(), Limiter: ratelimitmemory.New(c), Logger: log})
	if err != nil {
		return err
	}
	if args[0] == "up" {
		log.InfoContext(ctx, "running goauth migrations")
		return app.MigrateAuth(ctx, authn)
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

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	core, err := app.NewCore(ctx, app.Deps{Config: cfg, Logger: log})
	if err != nil {
		return err
	}
	defer core.Close()

	authSpec, err := core.Auth.GenerateOpenAPIDocs(types.OpenAPIConfig{Title: "Hookbeam auth", Version: cfg.App.Version, UseDefaultAuth: true})
	if err != nil {
		return fmt.Errorf("goauth openapi: %w", err)
	}
	h, err := handlers.New(handlers.Deps{Auth: core.Auth, APIKeys: core.APIKeys, DB: core.Pool, Delivery: core.Delivery, DeliveryHealth: core.DeliveryHealth, Members: core.Members, OrgPolicy: core.OrgPolicy, Audit: core.Audit, Notifications: core.Notifications, Authz: core.Authz, Limiter: core.RateLimiter, KeySubmitLimit: cfg.Limits.KeySubmissionsPerMinute, AccessCookie: config.AccessCookie, Logger: log})
	if err != nil {
		return err
	}
	router, err := handlers.NewRouter(h, handlers.RouterOptions{
		Logger:       log,
		MaxBodyBytes: cfg.HTTP.MaxBodyBytes,
		AuthRoutes:   core.Auth.Routes(),
		AuthSpec:     authSpec,
		AuthBasePath: config.AuthBasePath,
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
