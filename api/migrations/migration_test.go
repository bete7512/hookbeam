//go:build integration

package migrations_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/auth"
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
}

func TestAuthMigrationRoundTrip(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrator := migrations.New(slog.New(slog.DiscardHandler))
	require.NoError(t, migrator.Run(ctx, pool, []string{"down-to", "6"}))
	users, err := repos.NewUserRepo(pool)
	require.NoError(t, err)
	user := &models.User{Email: "migration@example.test", Name: "Legacy user"}
	require.NoError(t, users.CreateUser(ctx, user))
	legacySessionHash := auth.HashToken("migration-legacy-refresh")
	legacyActionHash := auth.HashToken("migration-legacy-action")
	_, err = pool.Exec(ctx, `INSERT INTO sessions (user_id, refresh_token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, user.ID, legacySessionHash)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO auth_tokens (user_id, purpose, token_hash, expires_at) VALUES ($1, $2, $3, now() + interval '1 hour')`, user.ID, models.TokenPurposePasswordReset, legacyActionHash)
	require.NoError(t, err)

	for _, phase := range []string{"upgrade existing data", "upgrade after rollback"} {
		t.Run(phase, func(t *testing.T) {
			require.NoError(t, migrator.Run(ctx, pool, []string{"up-to", "8"}))
			stored, err := users.GetUser(ctx, user.ID)
			require.NoError(t, err)
			require.Equal(t, user.Email, stored.Email)
			require.Equal(t, user.Name, stored.Name)
			require.Equal(t, user.TokenVersion, stored.TokenVersion)
			sessions, err := repos.NewSessionRepo(pool)
			require.NoError(t, err)
			legacy, err := sessions.FindSessionByToken(ctx, legacySessionHash)
			require.NoError(t, err)
			require.Equal(t, int32(-1), legacy.TokenVersion, "legacy sessions must have an invalid credential generation")
			require.NotEqual(t, stored.TokenVersion, legacy.TokenVersion)
			var requestID *int64
			require.NoError(t, pool.QueryRow(ctx, `SELECT request_id FROM auth_tokens WHERE token_hash=$1`, legacyActionHash).Scan(&requestID))
			require.Nil(t, requestID, "legacy links must not acquire a current delivery request")
			tokens, err := repos.NewAuthTokenRepo(pool)
			require.NoError(t, err)
			_, err = tokens.ConsumeAuthToken(ctx, legacyActionHash, models.TokenPurposePasswordReset)
			require.ErrorIs(t, err, apperr.ErrNotFound)

			// The partial index permits legacy rows while enforcing the new user/purpose slot.
			fresh := &models.AuthToken{
				UserID: user.ID, RequestID: 10, Purpose: models.TokenPurposePasswordReset,
				TokenHash: auth.HashToken("migration-current-" + phase), ExpiresAt: time.Now().Add(time.Hour),
			}
			require.NoError(t, tokens.CreateAuthToken(ctx, fresh))
			fresh.RequestID = 11
			fresh.TokenHash = auth.HashToken("migration-newer-" + phase)
			require.NoError(t, tokens.CreateAuthToken(ctx, fresh), "the conflict target must find the concurrently created unique index")
			var activeSlots int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM auth_tokens WHERE user_id=$1 AND purpose=$2 AND request_id IS NOT NULL`, user.ID, models.TokenPurposePasswordReset).Scan(&activeSlots))
			require.Equal(t, 1, activeSlots)
		})
		if phase == "upgrade existing data" {
			require.NoError(t, migrator.Run(ctx, pool, []string{"down-to", "6"}))
			var boundColumns int
			require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND ((table_name='sessions' AND column_name='token_version') OR (table_name='auth_tokens' AND column_name='request_id'))`).Scan(&boundColumns))
			require.Zero(t, boundColumns, "rollback must remove both new columns")
		}
	}
}
