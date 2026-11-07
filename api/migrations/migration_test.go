//go:build integration

package migrations_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/migrations"
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
	require.NoError(t, migrator.Run(ctx, pool, []string{"down-to", "0"}))
	var tables int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('outbox','processed_events','api_keys')`).Scan(&tables))
	require.Zero(t, tables, "down-to 0 must drop every Hookbeam table")
	require.NoError(t, migrator.Run(ctx, pool, []string{"up"}))
}
