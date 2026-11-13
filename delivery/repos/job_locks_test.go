//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type JobLockRepoTestSuite struct {
	suite.Suite
	db *pgxpool.Pool
}

func TestJobLockRepoTestSuite(t *testing.T) {
	suite.Run(t, new(JobLockRepoTestSuite))
}

func (s *JobLockRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), nil)
}

// seed is a second process: its own repo, so its own pinned connection, holding the given names.
func (s *JobLockRepoTestSuite) seed(names ...string) repos.JobLockRepo {
	other, err := repos.NewJobLockRepo(s.db)
	s.Require().NoError(err)
	for _, name := range names {
		held, err := other.Hold(context.Background(), name)
		s.Require().NoError(err)
		s.Require().True(held)
	}
	return other
}

func (s *JobLockRepoTestSuite) TestHold() {
	tests := []struct {
		name  string
		seed  []string
		holds []string // the names this process asks for, in order
		want  []bool
	}{
		{name: "a free lock is taken", holds: []string{"prune audit"}, want: []bool{true}},
		{name: "holding again keeps it", holds: []string{"prune audit", "prune audit"}, want: []bool{true, true}},
		{name: "held by another process", seed: []string{"prune audit"}, holds: []string{"prune audit"}, want: []bool{false}},
		{name: "another job is independent", seed: []string{"prune audit"}, holds: []string{"clean outbox"}, want: []bool{true}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			other := s.seed(tt.seed...)
			defer other.Close()
			repo, err := repos.NewJobLockRepo(s.db)
			s.Require().NoError(err)
			defer repo.Close()

			got := make([]bool, 0, len(tt.holds))
			for _, name := range tt.holds {
				held, err := repo.Hold(context.Background(), name)
				s.Require().NoError(err)
				got = append(got, held)
			}

			s.Equal(tt.want, got)
		})
	}
}

func (s *JobLockRepoTestSuite) TestClose() {
	s.Run("closing the holder frees its locks for the next process", func() {
		other := s.seed("prune audit")
		repo, err := repos.NewJobLockRepo(s.db)
		s.Require().NoError(err)
		defer repo.Close()
		held, err := repo.Hold(context.Background(), "prune audit")
		s.Require().NoError(err)
		s.Require().False(held)

		other.Close()

		held, err = repo.Hold(context.Background(), "prune audit")
		s.Require().NoError(err)
		s.True(held)
	})
	s.Run("a returned connection carries no lock into the pool", func() {
		other := s.seed("prune audit")
		other.Close()
		var stale int
		s.Require().NoError(s.db.QueryRow(context.Background(), `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND classid = 7001`).Scan(&stale))
		s.Zero(stale)
	})
}
