//go:build integration

package repos_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type HealthRepoTestSuite struct {
	suite.Suite
	db        *pgxpool.Pool
	apps      repos.ApplicationRepo
	endpoints repos.EndpointRepo
	repo      repos.HealthRepo
}

func TestHealthRepoTestSuite(t *testing.T) {
	suite.Run(t, new(HealthRepoTestSuite))
}

func (s *HealthRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.apps, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
	s.endpoints, err = repos.NewEndpointRepo(s.db)
	s.Require().NoError(err)
	s.repo, err = repos.NewHealthRepo(s.db)
	s.Require().NoError(err)
}

func (s *HealthRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "health_episodes", "endpoint_health", "endpoints", "applications", "environments")
}

func (s *HealthRepoTestSuite) seedEndpoint() *models.Endpoint {
	app := seedApplication(&s.Suite, s.db, s.apps, "Customer A")
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(s.endpoints.CreateEndpoint(context.Background(), e))
	return e
}

func (s *HealthRepoTestSuite) observe(e *models.Endpoint, failed bool, revision int32) (*models.EndpointHealth, error) {
	return s.repo.ObserveEndpointHealth(context.Background(), models.HealthObservation{EndpointID: e.ID, WorkspaceID: ws, ApplicationID: e.ApplicationID, Revision: revision, Failed: failed, ObservedAt: time.Now()})
}

func (s *HealthRepoTestSuite) TestObserveEndpointHealth() {
	s.Run("failures extend the streak, a success resets it, an older revision is ignored", func() {
		e := s.seedEndpoint()
		h, err := s.observe(e, true, 1)
		s.Require().NoError(err)
		s.Equal(int32(1), h.ConsecutiveFailures)
		s.Equal("failed", h.LastState)
		s.Nil(h.OpenEpisodeID)
		h, err = s.observe(e, true, 2)
		s.Require().NoError(err)
		s.Equal(int32(2), h.ConsecutiveFailures)
		s.Equal(int32(2), h.Revision)
		_, err = s.observe(e, true, 1)
		s.Require().ErrorIs(err, apperr.ErrNotFound, "a late attempt under revision 1 is ignored")
		h, err = s.observe(e, false, 2)
		s.Require().NoError(err)
		s.Zero(h.ConsecutiveFailures)
		s.Equal("succeeded", h.LastState)
	})
	s.Run("unknown endpoint is refused by the foreign key", func() {
		_, err := s.repo.ObserveEndpointHealth(context.Background(), models.HealthObservation{EndpointID: -1, WorkspaceID: ws, Revision: 1, ObservedAt: time.Now()})
		s.Error(err)
	})
}

func (s *HealthRepoTestSuite) TestEpisodes() {
	s.Run("open, continue, resolve; one open episode per endpoint; scoped reads", func() {
		e := s.seedEndpoint()
		_, err := s.observe(e, true, 1)
		s.Require().NoError(err)
		ep := &models.HealthEpisode{EndpointID: e.ID, WorkspaceID: ws, Revision: 1, ConsecutiveFailures: 3, OpenedAt: time.Now()}
		s.Require().NoError(s.repo.OpenEpisode(context.Background(), ep))
		s.Positive(ep.ID)
		s.Nil(ep.ResolvedAt)
		second := &models.HealthEpisode{EndpointID: e.ID, WorkspaceID: ws, Revision: 1, ConsecutiveFailures: 4, OpenedAt: time.Now()}
		s.Require().ErrorIs(s.repo.OpenEpisode(context.Background(), second), apperr.ErrConflict)

		h, err := s.repo.GetEndpointHealth(context.Background(), ws, e.ID)
		s.Require().NoError(err)
		s.Require().NotNil(h.OpenEpisodeID)
		s.Equal(ep.ID, *h.OpenEpisodeID)
		_, err = s.repo.GetEndpointHealth(context.Background(), "ws_other", e.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		_, err = s.repo.GetEndpointHealth(context.Background(), ws, -1)
		s.Require().ErrorIs(err, apperr.ErrNotFound)

		got, err := s.repo.GetEpisode(context.Background(), ws, ep.ID)
		s.Require().NoError(err)
		s.Equal(int32(3), got.ConsecutiveFailures)
		_, err = s.repo.GetEpisode(context.Background(), "ws_other", ep.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)

		s.Require().NoError(s.repo.ContinueEpisode(context.Background(), ep.ID, 4))
		s.Require().NoError(s.repo.ResolveEpisode(context.Background(), ep.ID, time.Now()))
		s.Require().ErrorIs(s.repo.ResolveEpisode(context.Background(), ep.ID, time.Now()), apperr.ErrNotFound, "resolving twice changes nothing")
		s.Require().ErrorIs(s.repo.ContinueEpisode(context.Background(), ep.ID, 5), apperr.ErrNotFound, "a resolved episode is closed")
		h, err = s.repo.GetEndpointHealth(context.Background(), ws, e.ID)
		s.Require().NoError(err)
		s.Nil(h.OpenEpisodeID)
		s.Require().NoError(s.repo.OpenEpisode(context.Background(), second), "a new episode may open after the first resolved")
	})
}
