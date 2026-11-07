//go:build integration

package services_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/apps/api/migrations"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/apps/api/services"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/testutil"
)

type rollbackRegistration struct{ db.Transactor }

func (r rollbackRegistration) WithTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return r.Transactor.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		return errors.New("abort after both writes")
	})
}

func TestRegistrationAtomic(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	for _, rollback := range []bool{false, true} {
		name := "commits with one connection"
		if rollback {
			name = "rollback leaves neither user nor event"
		}
		t.Run(name, func(t *testing.T) {
			testutil.Truncate(t, pool, "users", "outbox")
			cfg := pool.Config().Copy()
			cfg.MaxConns = 1
			cfg.MinConns = 0
			single, err := pgxpool.NewWithConfig(context.Background(), cfg)
			require.NoError(t, err)
			defer single.Close()
			users, err := repos.NewUserRepo(single)
			require.NoError(t, err)
			sessions, err := repos.NewSessionRepo(single)
			require.NoError(t, err)
			outbox, err := repos.NewOutboxRepo(single)
			require.NoError(t, err)
			tx := db.NewTransactor(single)
			if rollback {
				tx = rollbackRegistration{tx}
			}
			svc, err := services.NewUserService(services.UserServiceDeps{Users: users, Sessions: sessions, Outbox: outbox, Tx: tx, Logger: slog.New(slog.DiscardHandler), Hasher: auth.NewArgon2id(auth.Argon2idParams{Memory: 8192, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32})})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = svc.Register(ctx, &models.User{Email: "atomic@example.com", Name: "Atomic"}, "correct horse battery")
			if rollback {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var userCount, eventCount int
			require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&userCount))
			require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM outbox").Scan(&eventCount))
			want := 1
			if rollback {
				want = 0
			}
			require.Equal(t, want, userCount)
			require.Equal(t, want, eventCount)
		})
	}
}
