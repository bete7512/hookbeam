// Command worker relays the delivery outbox to the queue and prunes it; delivery attempts arrive with milestone 2.
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

	"github.com/bete7512/hookbeam/delivery/app"
	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/logger"
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

	core, err := app.NewCore(ctx, app.Deps{Config: cfg, Logger: log})
	if err != nil {
		return err
	}
	defer core.Close()

	log.InfoContext(ctx, "delivery worker started", "events_backend", cfg.Events.Backend)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return publishEvents(gctx, core.Events, cfg.Outbox.PollInterval, log) })
	g.Go(func() error { return cleanOutbox(gctx, core.Events, cleanInterval, log) })
	err = g.Wait()
	log.InfoContext(context.Background(), "delivery worker stopped")
	return err
}

// publishEvents drains the outbox now and after every interval until ctx is done.
func publishEvents(ctx context.Context, events services.EventService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := events.Publish(ctx); err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "publish events failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// cleanOutbox deletes expired outbox rows now and after every interval until ctx is done.
func cleanOutbox(ctx context.Context, events services.EventService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, cleanTimeout)
		_, err := events.CleanOutbox(cctx)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "clean outbox failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
