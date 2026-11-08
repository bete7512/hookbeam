//go:build integration

package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events/memory"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

// TestPublishConcurrent checks disjoint batches while all concurrent publishes succeed.
// Publish drains, so each worker calls it once; SKIP LOCKED is what keeps their batches disjoint.
func TestPublishConcurrent(t *testing.T) {
	const total, workers, batch = 50, 4, 7
	pool := testutil.Postgres(t, migrations.Up)
	outbox, err := repos.NewOutboxRepo(pool)
	require.NoError(t, err)
	for i := range total {
		e := &models.OutboxEvent{Type: "user.registered", Payload: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))}
		require.NoError(t, outbox.CreateOutboxEvent(context.Background(), e))
	}
	bus := memory.New(total * workers)
	svc := newEventService(t, pool, bus, batch)

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			if _, err := svc.Publish(context.Background()); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, total, bus.Len())
	rows, err := pool.Query(context.Background(), `SELECT id, type, payload, occurred_at, published_at, trace_parent FROM outbox ORDER BY id`)
	require.NoError(t, err)
	all, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.OutboxEvent])
	require.NoError(t, err)
	require.Len(t, all, total)
	for _, e := range all {
		require.NotNil(t, e.PublishedAt)
	}
}

func newEventService(t *testing.T, pool *pgxpool.Pool, bus *memory.Bus, batch int) services.EventService {
	t.Helper()
	outbox, err := repos.NewOutboxRepo(pool)
	require.NoError(t, err)
	processed, err := repos.NewProcessedEventRepo(pool)
	require.NoError(t, err)
	svc, err := services.NewEventService(services.EventServiceDeps{
		Tx: db.NewTransactor(pool), Outbox: outbox, Processed: processed, Publisher: bus, BatchSize: batch, Retention: time.Hour, Logger: slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	return svc
}

// TestProcessOnce checks the processed_events guard against a real table: a failed fn leaves no record.
func TestProcessOnce(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	svc := newEventService(t, pool, memory.New(1), 10)
	tests := []struct {
		name       string
		deliveries []error // fn result per delivery of the same event
		wantCalls  int
		wantFresh  []bool
	}{
		{name: "first delivery runs", deliveries: []error{nil}, wantCalls: 1, wantFresh: []bool{true}},
		{name: "redelivery is skipped", deliveries: []error{nil, nil}, wantCalls: 1, wantFresh: []bool{true, false}},
		{name: "failure is not recorded so a retry runs again", deliveries: []error{errors.New("boom"), nil}, wantCalls: 2, wantFresh: []bool{false, true}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			for d, result := range tt.deliveries {
				fresh, err := svc.ProcessOnce(context.Background(), int64(100+i), func(context.Context) error {
					calls++
					return result
				})
				require.Equal(t, result != nil, err != nil, "delivery %d: %v", d, err)
				require.Equal(t, tt.wantFresh[d], fresh, "delivery %d", d)
			}
			require.Equal(t, tt.wantCalls, calls)
		})
	}
}
