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
	"github.com/bete7512/hookbeam/delivery/app"
	"github.com/bete7512/hookbeam/delivery/config"
	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/rpc"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/grpcx"
	"github.com/bete7512/hookbeam/pkg/logger"
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

func serve(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	core, err := app.NewCore(ctx, app.Deps{Config: cfg, Logger: log})
	if err != nil {
		return err
	}
	defer core.Close()

	creds, err := cfg.TLS.ServerCredentials()
	if err != nil {
		return err
	}
	srv, err := rpc.New(rpc.Deps{Audit: core.Audit, Environments: core.Environments, Applications: core.Applications, Endpoints: core.Endpoints, Health: core.Health, Messages: core.Messages, Logger: log})
	if err != nil {
		return err
	}
	grpcServer := grpc.NewServer(grpc.Creds(creds), grpcx.ServerInterceptors(log))
	deliveryv1.RegisterDeliveryServiceServer(grpcServer, srv)
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
