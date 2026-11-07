// Package db owns the Postgres connection: pgx pool construction, transaction
// helpers, and mapping of pgx errors to apperr sentinels.
//
// Nothing here knows about tables; repos in <service>/repos do, and
// schema changes live in the top-level migrations package.
package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/hookbeam/pkg/apperr"
)

const (
	connectRetries       = 10
	connectRetryInterval = 5 * time.Second
)

// NewPool opens a pgxpool.Pool from cfg and pings it once so a bad URL or a
// down database fails at boot rather than on the first request. Callers own
// the pool and must Close it.
//
// Connection errors wrap apperr.ErrUnavailable: the database being unreachable is a dependency
// failure, and /readyz reports it as such rather than as a bug in our code.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("db: config: %w", err)
	}

	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("db: parse url: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	// Zero durations keep pgx's defaults (1h lifetime, 30m idle, 1m healthcheck,
	// no connect timeout), matching Config.Validate's contract.
	if cfg.MaxConnLifetime > 0 {
		pc.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		pc.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if cfg.ConnectTimeout > 0 {
		pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w: %w", apperr.ErrUnavailable, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: connect: %w: %w", apperr.ErrUnavailable, err)
	}
	return pool, nil
}

// NewPoolWithRetries is NewPool with a constant backoff: up to 10 attempts,
// 5s apart, so a service starting alongside its database (compose, ECS with
// a fresh RDS) does not crash-loop. Config errors are not retried — waiting
// will not fix a bad URL. ctx cancellation aborts the wait.
func NewPoolWithRetries(ctx context.Context, logger *slog.Logger, cfg Config) (*pgxpool.Pool, error) {
	var lastErr error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		pool, err := NewPool(ctx, cfg)
		if err == nil {
			return pool, nil
		}
		if !errors.Is(err, apperr.ErrUnavailable) {
			return nil, err
		}
		lastErr = err
		logger.WarnContext(ctx, "db connection attempt failed",
			"attempt", attempt, "max_attempts", connectRetries, "retry_in", connectRetryInterval, "err", err)

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("db: connect aborted: %w: %w", apperr.ErrUnavailable, ctx.Err())
		case <-time.After(connectRetryInterval):
		}
	}
	return nil, lastErr
}
