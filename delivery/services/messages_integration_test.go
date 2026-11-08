//go:build integration

package services_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

// TestBacklogCapLeavesNoRow fills a workspace's backlog to its cap and checks that the rejected submission wrote
// no message and no delivery: capacity never drops an accepted message, it refuses before accepting.
func TestBacklogCapLeavesNoRow(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Postgres(t, migrations.Up)
	testutil.Truncate(t, pool, "attempts", "deliveries", "messages", "health_episodes", "endpoint_health", "subscriptions", "endpoint_secrets", "endpoints", "applications", "environments", "outbox", "audit_events")
	apps, err := repos.NewApplicationRepo(pool)
	require.NoError(t, err)
	envs, err := repos.NewEnvironmentRepo(pool)
	require.NoError(t, err)
	endpoints, err := repos.NewEndpointRepo(pool)
	require.NoError(t, err)
	subs, err := repos.NewSubscriptionRepo(pool)
	require.NoError(t, err)
	messages, err := repos.NewMessageRepo(pool)
	require.NoError(t, err)
	deliveries, err := repos.NewDeliveryRepo(pool)
	require.NoError(t, err)
	attempts, err := repos.NewAttemptRepo(pool)
	require.NoError(t, err)
	outbox, err := repos.NewOutboxRepo(pool)
	require.NoError(t, err)
	audit, err := repos.NewAuditRepo(pool)
	require.NoError(t, err)

	env, err := envs.EnsureDefaultEnvironment(ctx, ws, actor)
	require.NoError(t, err)
	app := &models.Application{WorkspaceID: ws, EnvironmentID: env.ID, Name: "Customer A", CreatedBy: actor}
	require.NoError(t, apps.CreateApplication(ctx, app))
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	require.NoError(t, endpoints.CreateEndpoint(ctx, e))
	require.NoError(t, subs.CreateSubscriptions(ctx, e.ID, nil))

	limits := testLimits
	limits.BacklogPerWorkspace = 1
	svc, err := services.NewMessageService(services.MessageServiceDeps{
		Tx: db.NewTransactor(pool), Applications: apps, Environments: envs, Endpoints: endpoints, Messages: messages, Deliveries: deliveries, Attempts: attempts, Outbox: outbox, Audit: audit,
		Policy: services.DefaultPolicy, Limits: limits, PayloadRetention: time.Hour, IdempotencyWindow: time.Hour, MaxPayloadBytes: 1 << 16, ExpiryBatchSize: 10, Logger: slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	first, err := svc.Accept(ctx, scope, services.AcceptInput{ApplicationID: app.ID, EventType: "order.shipped", IdempotencyKey: "k1", Payload: []byte(`{"type":"order.shipped"}`)})
	require.NoError(t, err)
	require.Equal(t, int32(1), first.DeliveryCount)

	_, err = svc.Accept(ctx, scope, services.AcceptInput{ApplicationID: app.ID, EventType: "order.shipped", IdempotencyKey: "k2", Payload: []byte(`{"type":"order.shipped"}`)})
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 429, appErr.Code)
	require.Equal(t, limits.BacklogRetryAfter, appErr.RetryAfter)

	var msgs, dels int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&msgs))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM deliveries`).Scan(&dels))
	require.Equal(t, 1, msgs, "the rejected submission left no message row")
	require.Equal(t, 1, dels)

	// Finishing the backlog frees the slot.
	_, err = pool.Exec(ctx, `UPDATE deliveries SET state = 'succeeded', completed_at = now()`)
	require.NoError(t, err)
	_, err = svc.Accept(ctx, scope, services.AcceptInput{ApplicationID: app.ID, EventType: "order.shipped", IdempotencyKey: "k2", Payload: []byte(`{"type":"order.shipped"}`)})
	require.NoError(t, err)
}
