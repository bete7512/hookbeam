//go:build integration

package services_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

// TestHealthEpisodesUnderConcurrency has several workers record failures for one endpoint at the same time, each in
// its own transaction, and checks that exactly one episode opens and exactly one unhealthy event is announced.
func TestHealthEpisodesUnderConcurrency(t *testing.T) {
	const workers = 8
	ctx := context.Background()
	pool := testutil.Postgres(t, migrations.Up)
	testutil.Truncate(t, pool, "attempts", "deliveries", "messages", "health_episodes", "endpoint_health", "subscriptions", "endpoint_secrets", "endpoints", "applications", "environments", "outbox", "audit_events")
	apps, err := repos.NewApplicationRepo(pool)
	require.NoError(t, err)
	envs, err := repos.NewEnvironmentRepo(pool)
	require.NoError(t, err)
	endpoints, err := repos.NewEndpointRepo(pool)
	require.NoError(t, err)
	healthRepo, err := repos.NewHealthRepo(pool)
	require.NoError(t, err)
	outbox, err := repos.NewOutboxRepo(pool)
	require.NoError(t, err)
	env, err := envs.EnsureDefaultEnvironment(ctx, ws, actor)
	require.NoError(t, err)
	app := &models.Application{WorkspaceID: ws, EnvironmentID: env.ID, Name: "Customer A", CreatedBy: actor}
	require.NoError(t, apps.CreateApplication(ctx, app))
	endpoint := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	require.NoError(t, endpoints.CreateEndpoint(ctx, endpoint))

	svc, err := services.NewHealthService(services.HealthServiceDeps{Health: healthRepo, Outbox: outbox, Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	tx := db.NewTransactor(pool)
	observe := func(failed bool, revision int32) error {
		return tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return svc.Observe(ctx, tx, models.HealthObservation{EndpointID: endpoint.ID, WorkspaceID: ws, ApplicationID: app.ID, Revision: revision, Failed: failed, ObservedAt: time.Now()})
		})
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			if err := observe(true, 1); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var openEpisodes, allEpisodes, unhealthyEvents int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM health_episodes WHERE resolved_at IS NULL`).Scan(&openEpisodes))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM health_episodes`).Scan(&allEpisodes))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE type = 'endpoint.health_changed' AND payload->>'state' = 'unhealthy'`).Scan(&unhealthyEvents))
	require.Equal(t, 1, openEpisodes, "at most one open episode")
	require.Equal(t, 1, allEpisodes)
	require.Equal(t, 1, unhealthyEvents, "one announcement")
	h, err := healthRepo.GetEndpointHealth(ctx, ws, endpoint.ID)
	require.NoError(t, err)
	require.Equal(t, int32(workers), h.ConsecutiveFailures, "every failure counted once, in order")

	status, err := svc.Status(ctx, ws, endpoint.ID)
	require.NoError(t, err)
	require.Equal(t, models.HealthUnhealthy, status.State)

	// A success under a newer revision resolves the episode; a late failure under the old revision cannot reopen it.
	require.NoError(t, observe(false, 2))
	require.NoError(t, observe(true, 1))
	require.NoError(t, observe(true, 1))
	require.NoError(t, observe(true, 1))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM health_episodes WHERE resolved_at IS NULL`).Scan(&openEpisodes))
	require.Zero(t, openEpisodes, "stale failures reopen nothing")
	status, err = svc.Status(ctx, ws, endpoint.ID)
	require.NoError(t, err)
	require.Equal(t, models.HealthHealthy, status.State)
	require.Zero(t, status.ConsecutiveFailures)
	var healthyEvents int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE type = 'endpoint.health_changed' AND payload->>'state' = 'healthy'`).Scan(&healthyEvents))
	require.Equal(t, 1, healthyEvents)
}
