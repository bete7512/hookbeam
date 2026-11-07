//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func TestWithTx(t *testing.T) {
	ctx := context.Background()
	pool := testutil.Postgres(t, nil)
	_, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS withtx_items (name TEXT NOT NULL)")
	require.NoError(t, err)

	count := func(q db.DBTX) int {
		var n int
		require.NoError(t, q.QueryRow(ctx, "SELECT count(*) FROM withtx_items").Scan(&n))
		return n
	}
	insert := func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO withtx_items (name) VALUES ('x')")
		return err
	}

	t.Run("commit persists", func(t *testing.T) {
		require.NoError(t, db.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error { return insert(tx) }))
		require.Equal(t, 1, count(pool))
	})

	t.Run("error rolls back and is returned unchanged", func(t *testing.T) {
		sentinel := errors.New("business rule failed")
		err := db.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			require.NoError(t, insert(tx))
			require.Equal(t, 2, count(tx))
			return sentinel
		})
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, 1, count(pool))
	})

	t.Run("panic rolls back and propagates", func(t *testing.T) {
		require.PanicsWithValue(t, "boom", func() {
			_ = db.WithTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
				require.NoError(t, insert(tx))
				panic("boom")
			})
		})
		require.Equal(t, 1, count(pool))
	})

	t.Run("read-only option rejects writes", func(t *testing.T) {
		err := db.WithTxOptions(ctx, pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(ctx context.Context, tx pgx.Tx) error { return insert(tx) })
		require.Error(t, err)
		require.Equal(t, 1, count(pool))
	})
}
