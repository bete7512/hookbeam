//go:build integration

package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/bete7512/hookbeam/pkg/db"
)

var (
	once      sync.Once
	pool      *pgxpool.Pool
	container *tcpostgres.PostgresContainer
	setupErr  error
	dropDB    func() // drops the per-package database created from TEST_DATABASE_URL
)

// MigrateFunc applies a service's migrations, e.g. migrations.Up.
type MigrateFunc func(ctx context.Context, pool *pgxpool.Pool) error

// Postgres returns a pool shared by the test process, migrated with migrate when it is not nil.
// It uses TEST_DATABASE_URL when set, otherwise it starts a postgres container. Do not close the pool.
func Postgres(t testing.TB, migrate MigrateFunc) *pgxpool.Pool {
	t.Helper()
	once.Do(func() { setup(migrate) })
	if setupErr != nil {
		t.Fatalf("testutil: postgres: %v", setupErr)
	}
	return pool
}

// Main runs the tests and then stops the container. Use it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }
func Main(m *testing.M) int {
	code := m.Run()
	if pool != nil {
		pool.Close()
	}
	if dropDB != nil {
		dropDB()
	}
	if container != nil {
		_ = testcontainers.TerminateContainer(container)
	}
	return code
}

// Truncate empties tables and resets their identity sequences.
func Truncate(t testing.TB, p *pgxpool.Pool, tables ...string) {
	t.Helper()
	stmt := "TRUNCATE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := p.Exec(context.Background(), stmt); err != nil {
		t.Fatalf("testutil: %s: %v", stmt, err)
	}
}

func setup(migrate MigrateFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn != "" {
		var err error
		if dsn, err = createPackageDatabase(ctx, dsn); err != nil {
			setupErr = err
			return
		}
	} else {
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("hookbeam_test"),
			tcpostgres.WithUsername("hookbeam"),
			tcpostgres.WithPassword("hookbeam"),
			tcpostgres.BasicWaitStrategies(),
		)
		if err != nil {
			setupErr = fmt.Errorf("start container: %w", err)
			return
		}
		container = c
		if dsn, err = c.ConnectionString(ctx, "sslmode=disable"); err != nil {
			setupErr = fmt.Errorf("connection string: %w", err)
			return
		}
	}

	p, err := db.NewPool(ctx, db.Config{URL: dsn, MaxConns: 8, MinConns: 1})
	if err != nil {
		setupErr = err
		return
	}
	if migrate == nil {
		pool = p
		return
	}
	if err := migrate(ctx, p); err != nil {
		p.Close()
		setupErr = fmt.Errorf("migrate: %w", err)
		return
	}
	pool = p
}

// createPackageDatabase creates a database for this test binary on the server at baseURL,
// so packages testing in parallel never share one, and returns the URL to it.
func createPackageDatabase(ctx context.Context, baseURL string) (string, error) {
	sum := sha256.Sum256([]byte(os.Args[0]))
	name := "test_" + hex.EncodeToString(sum[:6])

	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		return "", err
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name); err != nil {
		admin.Close()
		return "", err
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close()
		return "", err
	}
	dropDB = func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name)
		admin.Close()
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}
