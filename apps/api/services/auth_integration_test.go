//go:build integration

package services_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/apps/api/migrations"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/apps/api/repos"
	"github.com/bete7512/scaffold/apps/api/services"
	"github.com/bete7512/scaffold/pkg/apperr"
	"github.com/bete7512/scaffold/pkg/auth"
	"github.com/bete7512/scaffold/pkg/cache"
	"github.com/bete7512/scaffold/pkg/db"
	"github.com/bete7512/scaffold/pkg/oidc"
	ratelimitmemory "github.com/bete7512/scaffold/pkg/ratelimit/memory"
	"github.com/bete7512/scaffold/pkg/testutil"
)

func newAuthIntegrationDeps(t *testing.T, pool *pgxpool.Pool) services.AuthServiceDeps {
	t.Helper()
	testutil.Truncate(t, pool, "users")
	users, err := repos.NewUserRepo(pool)
	require.NoError(t, err)
	sessions, err := repos.NewSessionRepo(pool)
	require.NoError(t, err)
	actionTokens, err := repos.NewAuthTokenRepo(pool)
	require.NoError(t, err)
	identities, err := repos.NewUserIdentityRepo(pool)
	require.NoError(t, err)
	outbox, err := repos.NewOutboxRepo(pool)
	require.NoError(t, err)
	tokens, err := auth.NewTokens(bytes.Repeat([]byte{1}, 32), "scaffold", time.Minute)
	require.NoError(t, err)
	appCache, err := cache.New(cache.Config{MaxItems: 1000})
	require.NoError(t, err)
	t.Cleanup(appCache.Close)
	return services.AuthServiceDeps{
		Users: users, Sessions: sessions, AuthTokens: actionTokens, Identities: identities, Outbox: outbox,
		Tx: db.NewTransactor(pool), Hasher: auth.NewArgon2id(auth.Argon2idParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}),
		Tokens: tokens, Limiter: ratelimitmemory.New(appCache), LoginLimit: 10, LoginWindow: time.Minute,
		RefreshTokenTTL: time.Hour, ActionTokenTTL: 30 * time.Minute, ActionTokenKey: bytes.Repeat([]byte{2}, 32),
		RecipientCooldown: time.Minute, Logger: slog.New(slog.DiscardHandler),
	}
}

func seedAuthUser(t *testing.T, deps services.AuthServiceDeps) *models.User {
	t.Helper()
	hash, err := deps.Hasher.Hash(password)
	require.NoError(t, err)
	user := &models.User{Email: "ada@example.com", Name: "Ada", PasswordHash: &hash}
	require.NoError(t, deps.Users.CreateUser(context.Background(), user))
	return user
}

type delayedPasswordCheck struct {
	auth.PasswordHasher
	checked chan struct{}
	resume  chan struct{}
}

func (h delayedPasswordCheck) Verify(password, hash string) (bool, error) {
	ok, err := h.PasswordHasher.Verify(password, hash)
	close(h.checked)
	<-h.resume
	return ok, err
}

func TestLoginCannotSurviveIdentityReclaim(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	deps := newAuthIntegrationDeps(t, pool)
	user := seedAuthUser(t, deps)
	checked, resume := make(chan struct{}), make(chan struct{})
	deps.Hasher = delayedPasswordCheck{PasswordHasher: deps.Hasher, checked: checked, resume: resume}
	svc, err := services.NewAuthService(deps)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := svc.Login(ctx, user.Email, password)
		result <- err
	}()
	select {
	case <-checked:
	case <-ctx.Done():
		close(resume)
		t.Fatal("login did not reach the credential check")
	}
	_, identityErr := svc.LoginWithIdentity(ctx, "google", oidc.Identity{
		Subject: "authoritative-subject", Email: user.Email, EmailVerified: true, HostedDomain: "example.com", Name: user.Name,
	})
	close(resume)
	require.NoError(t, identityErr)
	var loginErr error
	select {
	case loginErr = <-result:
	case <-ctx.Done():
		t.Fatal("delayed login did not finish")
	}
	var appErr *apperr.AppError
	require.ErrorAs(t, loginErr, &appErr)
	require.Equal(t, http.StatusUnauthorized, appErr.Code)

	// A late session written with the old generation can never refresh into the new generation.
	_, err = pool.Exec(ctx, `INSERT INTO sessions (user_id, token_version, refresh_token_hash, expires_at) VALUES ($1, 0, $2, now() + interval '1 hour')`, user.ID, auth.HashToken("late-refresh"))
	require.NoError(t, err)
	_, err = svc.Refresh(ctx, "late-refresh")
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, http.StatusUnauthorized, appErr.Code)
}

type failingVerificationRepo struct{ repos.UserRepo }

func (r failingVerificationRepo) WithTx(tx db.DBTX) repos.UserRepo {
	return failingVerificationRepo{UserRepo: r.UserRepo.WithTx(tx)}
}

func (r failingVerificationRepo) MarkEmailVerified(context.Context, int64) error {
	return errors.New("verification update failed")
}

func TestVerifyEmailRollsBackConsumption(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	deps := newAuthIntegrationDeps(t, pool)
	user := seedAuthUser(t, deps)
	svc, err := services.NewAuthService(deps)
	require.NoError(t, err)
	raw, err := svc.IssueActionToken(context.Background(), user.ID, models.TokenPurposeEmailVerification, 10)
	require.NoError(t, err)
	deps.Users = failingVerificationRepo{UserRepo: deps.Users}
	failing, err := services.NewAuthService(deps)
	require.NoError(t, err)
	require.Error(t, failing.VerifyEmail(context.Background(), raw))
	require.NoError(t, svc.VerifyEmail(context.Background(), raw), "failed verification must leave the token usable")
	stored, err := deps.Users.GetUser(context.Background(), user.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.EmailVerifiedAt)
	require.Error(t, svc.VerifyEmail(context.Background(), raw), "successful verification must consume the token")
}

func TestActionTokenSurvivesRetryAndRestart(t *testing.T) {
	pool := testutil.Postgres(t, migrations.Up)
	deps := newAuthIntegrationDeps(t, pool)
	user := seedAuthUser(t, deps)
	svc, err := services.NewAuthService(deps)
	require.NoError(t, err)
	ctx := context.Background()
	first, err := svc.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 10)
	require.NoError(t, err)
	require.NotEmpty(t, first)
	var expiry time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at FROM auth_tokens WHERE user_id=$1`, user.ID).Scan(&expiry))
	restarted, err := services.NewAuthService(deps)
	require.NoError(t, err)
	retry, err := restarted.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 10)
	require.NoError(t, err)
	require.Equal(t, first, retry)
	var retryExpiry time.Time
	require.NoError(t, pool.QueryRow(ctx, `SELECT expires_at FROM auth_tokens WHERE user_id=$1`, user.ID).Scan(&retryExpiry))
	require.Equal(t, expiry, retryExpiry)
	require.NoError(t, restarted.VerifyEmail(ctx, first))
	used, err := svc.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 10)
	require.NoError(t, err)
	require.Empty(t, used)
	newer, err := svc.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 11)
	require.NoError(t, err)
	require.NotEmpty(t, newer)
	require.NotEqual(t, first, newer)
	stale, err := svc.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 9)
	require.NoError(t, err)
	require.Empty(t, stale)
	_, err = pool.Exec(ctx, `UPDATE auth_tokens SET expires_at=now()-interval '1 second' WHERE user_id=$1`, user.ID)
	require.NoError(t, err)
	expired, err := svc.IssueActionToken(ctx, user.ID, models.TokenPurposeEmailVerification, 11)
	require.NoError(t, err)
	require.Empty(t, expired)
}
