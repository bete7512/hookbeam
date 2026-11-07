//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type ProcessedEventRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.ProcessedEventRepo
}

func TestProcessedEventRepoTestSuite(t *testing.T) {
	suite.Run(t, new(ProcessedEventRepoTestSuite))
}

func (s *ProcessedEventRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewProcessedEventRepo(s.db)
	s.Require().NoError(err)
}

func (s *ProcessedEventRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "processed_events")
}

func (s *ProcessedEventRepoTestSuite) TestMarkEventProcessed() {
	tests := []struct {
		name    string
		seed    []int64
		eventID int64
		want    bool
	}{
		{name: "first time is new", eventID: 1, want: true},
		{name: "second time is a replay", seed: []int64{1}, eventID: 1, want: false},
		{name: "other ids do not collide", seed: []int64{1}, eventID: 2, want: true},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			for _, id := range tt.seed {
				_, err := s.repo.MarkEventProcessed(context.Background(), id)
				s.Require().NoError(err)
			}

			got, err := s.repo.MarkEventProcessed(context.Background(), tt.eventID)

			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
}
