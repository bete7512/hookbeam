//go:build integration

package services_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

// plane is the data plane's services over a real database, for the flows that only a transaction can prove.
type plane struct {
	pool      *pgxpool.Pool
	apps      services.ApplicationService
	endpoints services.EndpointService
	messages  services.MessageService
}

func newPlane(t *testing.T) *plane {
	t.Helper()
	pool := testutil.Postgres(t, migrations.Up)
	// The package's tests share one database: start clean and leave nothing behind for the next one.
	truncate := func() {
		testutil.Truncate(t, pool, "attempts", "deliveries", "messages", "health_episodes", "endpoint_health", "subscriptions", "endpoint_secrets", "endpoints", "applications", "environments", "outbox", "audit_events")
	}
	truncate()
	t.Cleanup(truncate)
	must := func(err error) {
		t.Helper()
		require.NoError(t, err)
	}
	appRepo, err := repos.NewApplicationRepo(pool)
	must(err)
	envRepo, err := repos.NewEnvironmentRepo(pool)
	must(err)
	endpointRepo, err := repos.NewEndpointRepo(pool)
	must(err)
	secretRepo, err := repos.NewEndpointSecretRepo(pool)
	must(err)
	subRepo, err := repos.NewSubscriptionRepo(pool)
	must(err)
	messageRepo, err := repos.NewMessageRepo(pool)
	must(err)
	deliveryRepo, err := repos.NewDeliveryRepo(pool)
	must(err)
	attemptRepo, err := repos.NewAttemptRepo(pool)
	must(err)
	healthRepo, err := repos.NewHealthRepo(pool)
	must(err)
	outboxRepo, err := repos.NewOutboxRepo(pool)
	must(err)
	auditRepo, err := repos.NewAuditRepo(pool)
	must(err)
	key := make([]byte, 32)
	_, err = rand.Read(key)
	must(err)
	cipher, err := secrets.New(base64.StdEncoding.EncodeToString(key))
	must(err)
	log := slog.New(slog.DiscardHandler)
	tx := db.NewTransactor(pool)

	p := &plane{pool: pool}
	health, err := services.NewHealthService(services.HealthServiceDeps{Health: healthRepo, Outbox: outboxRepo, Logger: log})
	must(err)
	p.apps, err = services.NewApplicationService(services.ApplicationServiceDeps{
		Tx:           tx,
		Applications: appRepo,
		Environments: envRepo,
		Endpoints:    endpointRepo,
		Messages:     messageRepo,
		Deliveries:   deliveryRepo,
		Attempts:     attemptRepo,
		Health:       healthRepo,
		Audit:        auditRepo,
		Logger:       log,
	})
	must(err)
	p.endpoints, err = services.NewEndpointService(services.EndpointServiceDeps{
		Tx:            tx,
		Applications:  appRepo,
		Endpoints:     endpointRepo,
		Deliveries:    deliveryRepo,
		Audit:         auditRepo,
		Secrets:       secretRepo,
		Subscriptions: subRepo,
		Health:        health,
		Limits:        testLimits,
		Cipher:        cipher,
		KeyID:         "2026-11",
		URL:           safehttp.Config{AllowPrivate: true},
		Logger:        log,
	})
	must(err)
	p.messages, err = services.NewMessageService(services.MessageServiceDeps{
		Tx:                tx,
		Applications:      appRepo,
		Environments:      envRepo,
		Endpoints:         endpointRepo,
		Messages:          messageRepo,
		Deliveries:        deliveryRepo,
		Attempts:          attemptRepo,
		Outbox:            outboxRepo,
		Audit:             auditRepo,
		Policy:            services.DefaultPolicy,
		Limits:            testLimits,
		PayloadRetention:  time.Hour,
		IdempotencyWindow: time.Hour,
		MaxPayloadBytes:   1 << 16,
		ExpiryBatchSize:   10,
		Logger:            log,
	})
	must(err)
	return p
}

// count runs a count query.
func (p *plane) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, p.pool.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// TestEndpointHeadersInTheDatabase follows a header set through create, merge and a refused change: the value is
// never stored in the clear or audited, and a refused merge rolls the revision bump back.
func TestEndpointHeadersInTheDatabase(t *testing.T) {
	const canary = "CANARY-hdr-5d20af"
	ctx := context.Background()
	p := newPlane(t)
	app, err := p.apps.CreateApplication(ctx, scope, 0, "Checkout")
	require.NoError(t, err)

	e, _, _, err := p.endpoints.CreateEndpoint(ctx, scope, services.CreateEndpointInput{
		ApplicationID: app.ID,
		URL:           "https://receiver.example.com/hooks",
		Headers:       map[string]string{"Authorization": "Bearer " + canary},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"authorization"}, e.HeaderNames)

	e, err = p.endpoints.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{
		EndpointID:    e.ID,
		SetHeaders:    map[string]string{"X-Tenant": "acme"},
		RemoveHeaders: []string{"x-absent"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"authorization", "x-tenant"}, e.HeaderNames, "the untouched header kept its value")
	require.Equal(t, int32(2), e.Revision)

	read, err := p.endpoints.GetEndpoint(ctx, scope, e.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"authorization", "x-tenant"}, read.HeaderNames)

	tooMany := map[string]string{}
	for i := range 19 {
		tooMany[fmt.Sprintf("x-h-%d", i)] = canary
	}
	_, err = p.endpoints.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{EndpointID: e.ID, SetHeaders: tooMany})
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 422, appErr.Code)
	require.Contains(t, appErr.Fields, "headers")
	read, err = p.endpoints.GetEndpoint(ctx, scope, e.ID)
	require.NoError(t, err)
	require.Equal(t, int32(2), read.Revision, "the refused change rolled the revision bump back")
	require.Equal(t, []string{"authorization", "x-tenant"}, read.HeaderNames)

	stale := int32(1)
	_, err = p.endpoints.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{EndpointID: e.ID, SetHeaders: map[string]string{"x-late": "1"}, ExpectedRevision: &stale})
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.Code, "a header change honours the expected revision")

	e, err = p.endpoints.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{EndpointID: e.ID, RemoveHeaders: []string{"Authorization", "x-tenant"}})
	require.NoError(t, err)
	require.Empty(t, e.HeaderNames)
	require.Equal(t, 1, p.count(t, `SELECT count(*) FROM endpoints WHERE id = $1 AND headers_ciphertext IS NULL AND headers_key_id = ''`, e.ID), "an empty set is NULL again")

	require.Zero(t, p.count(t, `SELECT count(*) FROM endpoints WHERE position($1::bytea in headers_ciphertext) > 0`, []byte(canary)), "never in the clear")
	require.Zero(t, p.count(t, `SELECT count(*) FROM audit_events WHERE details::text LIKE '%' || $1 || '%'`, canary), "never audited")
	require.Equal(t, 2, p.count(t, `SELECT count(*) FROM audit_events WHERE action = 'endpoint.update' AND details ? 'header_names'`), "the two applied changes are audited by name; the refused ones left no row")
}

// TestPurgeApplicationInTheDatabase archives and purges an application that has an endpoint, a message, deliveries
// and an attempt, and checks every row is gone, the audit row stays, and the preconditions refuse first.
func TestPurgeApplicationInTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := newPlane(t)
	app, err := p.apps.CreateApplication(ctx, scope, 0, "Checkout")
	require.NoError(t, err)
	keep, err := p.apps.CreateApplication(ctx, scope, 0, "Billing")
	require.NoError(t, err)
	for _, a := range []*models.Application{app, keep} {
		_, _, _, err := p.endpoints.CreateEndpoint(ctx, scope, services.CreateEndpointInput{ApplicationID: a.ID, URL: "https://receiver.example.com/hooks", Headers: map[string]string{"x-api-key": "k"}})
		require.NoError(t, err)
		for _, key := range []string{"k1", "k2"} {
			_, err = p.messages.Accept(ctx, scope, services.AcceptInput{ApplicationID: a.ID, EventType: "order.shipped", IdempotencyKey: key, Payload: []byte(`{"type":"order.shipped"}`)})
			require.NoError(t, err)
		}
	}
	appRows := func(id int64) (endpoints, messages, deliveries int) {
		return p.count(t, `SELECT count(*) FROM endpoints WHERE application_id = $1`, id),
			p.count(t, `SELECT count(*) FROM messages WHERE application_id = $1`, id),
			p.count(t, `SELECT count(*) FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE m.application_id = $1`, id)
	}
	conflict := func(err error) string {
		t.Helper()
		var appErr *apperr.AppError
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, 409, appErr.Code)
		return appErr.Message
	}

	_, err = p.apps.PurgeApplication(ctx, scope, app.ID)
	require.Contains(t, conflict(err), "archive the application first")

	// One delivery is mid-attempt when the application is archived: the archive leaves it to settle.
	_, err = p.pool.Exec(ctx, `UPDATE deliveries SET state = 'in_flight', attempt_count = 1, lease_token = 'x', lease_expires_at = now() + interval '1 minute'
		WHERE id = (SELECT min(d.id) FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE m.application_id = $1)`, app.ID)
	require.NoError(t, err)
	_, archivedEndpoints, cancelled, err := p.apps.ArchiveApplication(ctx, scope, app.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), archivedEndpoints)
	require.Equal(t, int64(1), cancelled)

	_, err = p.apps.PurgeApplication(ctx, scope, app.ID)
	require.Contains(t, conflict(err), "1 attempt is still in flight")
	endpoints, messages, deliveries := appRows(app.ID)
	require.Equal(t, []int{1, 2, 2}, []int{endpoints, messages, deliveries}, "a refused purge deletes nothing")

	// The attempt settles and records.
	_, err = p.pool.Exec(ctx, `INSERT INTO attempts (delivery_id, number, endpoint_revision, secret_version, request_url, started_at)
		SELECT id, 1, 1, 1, 'https://receiver.example.com/hooks', now() FROM deliveries WHERE state = 'in_flight'`)
	require.NoError(t, err)
	_, err = p.pool.Exec(ctx, `UPDATE deliveries SET state = 'succeeded', lease_token = NULL, lease_expires_at = NULL, completed_at = now() WHERE state = 'in_flight'`)
	require.NoError(t, err)

	_, err = p.apps.PurgeApplication(ctx, services.Scope{WorkspaceID: "ws_other", ActorID: actor}, app.ID)
	var appErr *apperr.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.Code, "another workspace cannot purge it")

	purged, err := p.apps.PurgeApplication(ctx, scope, app.ID)
	require.NoError(t, err)
	require.Equal(t, models.ApplicationPurge{ApplicationID: app.ID, Endpoints: 1, Messages: 2, Deliveries: 2}, *purged)

	endpoints, messages, deliveries = appRows(app.ID)
	require.Equal(t, []int{0, 0, 0}, []int{endpoints, messages, deliveries})
	require.Zero(t, p.count(t, `SELECT count(*) FROM applications WHERE id = $1`, app.ID))
	require.Zero(t, p.count(t, `SELECT count(*) FROM attempts a WHERE NOT EXISTS (SELECT 1 FROM deliveries d WHERE d.id = a.delivery_id)`), "no orphaned attempt")
	require.Zero(t, p.count(t, `SELECT count(*) FROM endpoint_secrets s WHERE NOT EXISTS (SELECT 1 FROM endpoints e WHERE e.id = s.endpoint_id)`), "secrets went with their endpoint")
	require.Equal(t, 1, p.count(t, `SELECT count(*) FROM audit_events WHERE action = 'application.delete' AND resource_id = $1 AND details @> '{"deleted_endpoints":1,"deleted_messages":2,"deleted_deliveries":2}'`, app.ID), "the audit row outlives the application")
	require.Positive(t, p.count(t, `SELECT count(*) FROM audit_events WHERE action = 'application.archive' AND resource_id = $1`, app.ID), "earlier audit rows stay")

	endpoints, messages, deliveries = appRows(keep.ID)
	require.Equal(t, []int{1, 2, 2}, []int{endpoints, messages, deliveries}, "the other application is untouched")

	_, err = p.apps.PurgeApplication(ctx, scope, app.ID)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.Code, "purging what is gone is not found")
}
