//go:build integration

package repos_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type EndpointHealthRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.EndpointHealthRepo
}

func TestEndpointHealthRepoTestSuite(t *testing.T) {
	suite.Run(t, new(EndpointHealthRepoTestSuite))
}

func (s *EndpointHealthRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewEndpointHealthRepo(s.db)
	s.Require().NoError(err)
}

func (s *EndpointHealthRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "endpoint_health")
}

func (s *EndpointHealthRepoTestSuite) observe(at time.Time, state string, failed bool) {
	s.T().Helper()
	o := models.HealthObservation{EndpointID: 3, WorkspaceID: "ws_1", ApplicationID: 7, DeliveryID: 5, Attempt: 1, State: state, ObservedAt: at, CountsAsFailed: failed}
	s.Require().NoError(s.repo.ObserveEndpointHealth(context.Background(), o))
}

func (s *EndpointHealthRepoTestSuite) TestObserveEndpointHealth() {
	ctx := context.Background()
	t0 := time.Now().Add(-time.Minute)
	s.Run("failures extend the streak, a success resets it, a refresh keeps it", func() {
		s.observe(t0, "retry_scheduled", true)
		s.observe(t0.Add(time.Second), "retry_scheduled", true)
		s.observe(t0.Add(2*time.Second), "cancelled", false)
		got, err := s.repo.GetEndpointHealth(ctx, "ws_1", 3)
		s.Require().NoError(err)
		s.Equal(int32(2), got.ConsecutiveFailures)
		s.Equal("cancelled", got.LastState)

		s.observe(t0.Add(3*time.Second), "succeeded", false)
		got, err = s.repo.GetEndpointHealth(ctx, "ws_1", 3)
		s.Require().NoError(err)
		s.Zero(got.ConsecutiveFailures)
		s.Equal("succeeded", got.LastState)
	})
	s.Run("an older observation never overwrites a newer row", func() {
		s.observe(t0.Add(10*time.Second), "succeeded", false)
		s.observe(t0, "failed", true)
		got, err := s.repo.GetEndpointHealth(ctx, "ws_1", 3)
		s.Require().NoError(err)
		s.Equal("succeeded", got.LastState)
		s.Zero(got.ConsecutiveFailures)
		s.WithinDuration(t0.Add(10*time.Second), got.ObservedAt, time.Second)
	})
	s.Run("another workspace is not found", func() {
		s.observe(t0, "succeeded", false)
		_, err := s.repo.GetEndpointHealth(ctx, "ws_other", 3)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}
