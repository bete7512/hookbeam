// Package migrations holds the schema migrations, compiled into the delivery server binary and run with goose.
package migrations

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Service runs goose commands such as up, down and status.
type Service interface {
	Run(ctx context.Context, pool *pgxpool.Pool, args []string) error
}

type service struct {
	logger *slog.Logger
}

// registerOnce prevents goose from panicking when New is called twice in one process.
var registerOnce sync.Once

// New returns a Service with every migration registered.
func New(logger *slog.Logger) Service {
	s := &service{logger: logger}
	registerOnce.Do(s.registerMigrations)
	return s
}

// Up applies all pending migrations.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	return New(slog.New(slog.DiscardHandler)).Run(ctx, pool, []string{"up"})
}

// Run executes args[0] as a goose command with args[1:] as its arguments.
func (s *service) Run(ctx context.Context, pool *pgxpool.Pool, args []string) error {
	if len(args) == 0 {
		return errors.New("migrations: no command given")
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	s.logger.InfoContext(ctx, "running migrations", "command", args[0])
	return goose.RunContext(ctx, args[0], db, ".", args[1:]...)
}

// registerMigrations lists every migration in order.
func (s *service) registerMigrations() {
	s.registerMigration_0001_Baseline()
	s.registerMigration_0002_InflightEndpointIndex()
}
