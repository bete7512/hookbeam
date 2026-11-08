//go:build integration

package migrations_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func TestMigrations(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Postgres(t, migrations.Up)
	logger := slog.New(slog.DiscardHandler)

	require.NoError(t, migrations.Up(ctx, pool), "second up is a no-op")
	require.NoError(t, migrations.New(logger).Run(ctx, pool, []string{"status"}), "New twice must not re-register")
	require.Error(t, migrations.New(logger).Run(ctx, pool, nil), "no command")

	migrator := migrations.New(logger)

	// Backfill: an application created before 0004 ends up under a development environment of its workspace.
	require.NoError(t, migrator.Run(ctx, pool, []string{"down-to", "3"}))
	_, err := pool.Exec(ctx, `INSERT INTO applications (workspace_id, name, created_by) VALUES ('ws_old', 'Legacy', 'u_1')`)
	require.NoError(t, err)
	require.NoError(t, migrator.Run(ctx, pool, []string{"up"}))
	var kind string
	require.NoError(t, pool.QueryRow(ctx, `SELECT e.kind FROM applications a JOIN environments e ON e.id = a.environment_id WHERE a.name = 'Legacy'`).Scan(&kind))
	require.Equal(t, "development", kind)
	_, err = pool.Exec(ctx, `DELETE FROM applications WHERE name = 'Legacy'`)
	require.NoError(t, err)

	require.NoError(t, migrator.Run(ctx, pool, []string{"down-to", "0"}))
	var tables int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('audit_events','environments','applications','endpoints','endpoint_secrets','subscriptions','messages','deliveries','attempts','outbox','processed_events')`).Scan(&tables))
	require.Zero(t, tables, "down-to 0 must drop every delivery table")
	require.NoError(t, migrator.Run(ctx, pool, []string{"up"}))
}
