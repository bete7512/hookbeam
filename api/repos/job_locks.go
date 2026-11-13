package repos

//go:generate mockgen -source=job_locks.go -destination=repo_mocks/mock_job_locks.go -package=repo_mocks

import (
	"context"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bete7512/hookbeam/pkg/db"
)

// JobLockRepo holds the worker's per-job locks: Postgres session advisory locks on one connection pinned from the
// pool. A lock lasts as long as that connection, so a replica that dies hands its jobs over by itself, and nothing
// is written to a table. It satisfies pkg/worker's Locks port.
type JobLockRepo interface {
	// Hold takes the lock on name or confirms this process still holds it; false means another process has it.
	Hold(ctx context.Context, name string) (bool, error)
	// Close unlocks everything and returns the connection to the pool.
	Close()
}

// jobLockClass is the first half of every job lock key; the second half is the hashed job name.
const jobLockClass = 7001

type jobLockRepo struct {
	pool *pgxpool.Pool
	mu   sync.Mutex
	conn *pgxpool.Conn
}

// NewJobLockRepo returns a JobLockRepo over the pool; the connection is pinned on the first Hold.
func NewJobLockRepo(pool *pgxpool.Pool) (JobLockRepo, error) {
	if pool == nil {
		return nil, errors.New("repos: pool is required")
	}
	return &jobLockRepo{pool: pool}, nil
}

func (r *jobLockRepo) Hold(ctx context.Context, name string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil && r.conn.Conn().IsClosed() {
		r.drop() // the locks went with the connection
	}
	if r.conn == nil {
		conn, err := r.pool.Acquire(ctx)
		if err != nil {
			return false, db.HandleError(err)
		}
		r.conn = conn
	}
	var held bool
	query := `SELECT pg_try_advisory_lock(@class, hashtext(@name))`
	err := r.conn.QueryRow(ctx, query, pgx.NamedArgs{"class": jobLockClass, "name": name}).Scan(&held)
	if err != nil {
		// Whatever the failure, this connection can no longer be trusted to hold anything: the next Hold starts over.
		r.drop()
		return false, db.HandleError(err)
	}
	return held, nil
}

func (r *jobLockRepo) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		return
	}
	_, _ = r.conn.Exec(context.Background(), `SELECT pg_advisory_unlock_all()`)
	r.drop()
}

// drop closes the pinned connection so the pool discards it instead of handing its locks to the next borrower.
func (r *jobLockRepo) drop() {
	_ = r.conn.Conn().Close(context.Background())
	r.conn.Release()
	r.conn = nil
}
