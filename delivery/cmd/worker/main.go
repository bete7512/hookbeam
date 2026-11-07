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

	"github.com/bete7512/hookbeam/delivery/app"
	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/events"
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

	log.InfoContext(ctx, "delivery worker started", "events_backend", cfg.Events.Backend, "claim_interval", cfg.Claims.Interval)
	wake := make(chan struct{}, 1)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return publishEvents(gctx, core.Events, cfg.Outbox.PollInterval, log) })
	g.Go(func() error { return cleanOutbox(gctx, core.Events, cleanInterval, log) })
	g.Go(func() error { return dispatchDue(gctx, core.Dispatch, cfg.Claims.Interval, wake, log) })
	g.Go(func() error { return sweepLeases(gctx, core.Dispatch, cfg.Claims.SweepInterval, log) })
	g.Go(func() error { return expireRetention(gctx, core.Messages, cfg.Payload.ExpiryInterval, log) })
	g.Go(func() error {
		// Every event on the queue is a wake-up: claim whatever is due, so duplicates and late deliveries are harmless.
		return core.Subscriber.Run(gctx, func(context.Context, events.Event) error {
			select {
			case wake <- struct{}{}:
			default:
			}
			return nil
		})
	})
	err = g.Wait()
	log.InfoContext(context.Background(), "waiting for attempts in progress", "in_flight_bound", cfg.Claims.Concurrency)
	core.Dispatch.Drain()
	log.InfoContext(context.Background(), "delivery worker stopped")
	return err
}

// dispatchDue claims and attempts due deliveries now, on every wake-up and after every interval, draining
// until a batch comes back short.
func dispatchDue(ctx context.Context, dispatch services.DispatchService, interval time.Duration, wake <-chan struct{}, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			n, err := dispatch.Dispatch(ctx)
			if err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "dispatch failed", "error", err)
			}
			if n == 0 || err != nil || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		case <-ticker.C:
		}
	}
}

// sweepLeases recovers deliveries whose lease expired without an outcome, now and after every interval,
// draining until a batch comes back short.
func sweepLeases(ctx context.Context, dispatch services.DispatchService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			n, err := dispatch.Sweep(ctx)
			if err != nil && ctx.Err() == nil {
				log.WarnContext(ctx, "sweep failed", "error", err)
			}
			if n == 0 || err != nil || ctx.Err() != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// expireRetention empties payloads and clears idempotency keys past their retention, now and after every interval.
func expireRetention(ctx context.Context, messages services.MessageService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, cleanTimeout)
		payloads, err := messages.ExpirePayloads(cctx)
		if err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "expire payloads failed", "error", err)
		}
		keys, err := messages.ExpireIdempotencyKeys(cctx)
		if err != nil && ctx.Err() == nil {
			log.WarnContext(ctx, "expire idempotency keys failed", "error", err)
		}
		cancel()
		if payloads > 0 || keys > 0 {
			log.InfoContext(ctx, "retention applied", "payloads_emptied", payloads, "keys_cleared", keys)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// publishEvents drains the outbox now and after every interval until ctx is done.
func publishEvents(ctx context.Context, svc services.EventService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := svc.Publish(ctx); err != nil && ctx.Err() == nil {
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
func cleanOutbox(ctx context.Context, svc services.EventService, interval time.Duration, log *slog.Logger) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, cleanTimeout)
		_, err := svc.CleanOutbox(cctx)
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
