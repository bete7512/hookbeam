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
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type EndpointSecretRepoTestSuite struct {
	suite.Suite
	db        *pgxpool.Pool
	apps      repos.ApplicationRepo
	endpoints repos.EndpointRepo
	repo      repos.EndpointSecretRepo
}

func TestEndpointSecretRepoTestSuite(t *testing.T) {
	suite.Run(t, new(EndpointSecretRepoTestSuite))
}

func (s *EndpointSecretRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.apps, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
	s.endpoints, err = repos.NewEndpointRepo(s.db)
	s.Require().NoError(err)
	s.repo, err = repos.NewEndpointSecretRepo(s.db)
	s.Require().NoError(err)
}

func (s *EndpointSecretRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "endpoint_secrets", "endpoints", "applications")
}

func (s *EndpointSecretRepoTestSuite) seedEndpoint() *models.Endpoint {
	app := seedApplication(&s.Suite, s.apps, "Customer A")
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(s.endpoints.CreateEndpoint(context.Background(), e))
	return e
}

func (s *EndpointSecretRepoTestSuite) TestCreateEndpointSecret() {
	s.Run("versions count up per endpoint", func() {
		e := s.seedEndpoint()
		other := s.seedEndpointNamed("Customer B")
		for want := int32(1); want <= 3; want++ {
			sec := &models.EndpointSecret{EndpointID: e.ID, Ciphertext: []byte("ct"), KeyID: "2026-11", Fingerprint: "abcd"}
			s.Require().NoError(s.repo.CreateEndpointSecret(context.Background(), sec))
			s.Equal(want, sec.Version)
			s.Positive(sec.ID)
			s.Nil(sec.RetireAt)
		}
		sec := &models.EndpointSecret{EndpointID: other.ID, Ciphertext: []byte("ct"), KeyID: "2026-11", Fingerprint: "abcd"}
		s.Require().NoError(s.repo.CreateEndpointSecret(context.Background(), sec))
		s.Equal(int32(1), sec.Version, "versions are per endpoint")
	})
	s.Run("unknown endpoint fails", func() {
		sec := &models.EndpointSecret{EndpointID: -1, Ciphertext: []byte("ct"), KeyID: "k", Fingerprint: "f"}
		s.Error(s.repo.CreateEndpointSecret(context.Background(), sec))
	})
}

func (s *EndpointSecretRepoTestSuite) seedEndpointNamed(appName string) *models.Endpoint {
	app := seedApplication(&s.Suite, s.apps, appName)
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(s.endpoints.CreateEndpoint(context.Background(), e))
	return e
}

func (s *EndpointSecretRepoTestSuite) TestRetireEndpointSecrets() {
	ctx := context.Background()
	s.Run("retires every other secret, keeps an earlier retire_at, stops listing retired ones", func() {
		e := s.seedEndpoint()
		var versions []*models.EndpointSecret
		for i := 0; i < 3; i++ {
			sec := &models.EndpointSecret{EndpointID: e.ID, Ciphertext: []byte("ct"), KeyID: "k", Fingerprint: "f"}
			s.Require().NoError(s.repo.CreateEndpointSecret(ctx, sec))
			versions = append(versions, sec)
		}
		soon := time.Now().Add(time.Hour)
		n, err := s.repo.RetireEndpointSecrets(ctx, e.ID, 2, soon)
		s.Require().NoError(err)
		s.Equal(int64(2), n)

		later := time.Now().Add(24 * time.Hour)
		n, err = s.repo.RetireEndpointSecrets(ctx, e.ID, 3, later)
		s.Require().NoError(err)
		s.Equal(int64(1), n, "only version 2 moves; version 1 keeps its earlier retire_at")

		active, err := s.repo.ListActiveEndpointSecrets(ctx, e.ID)
		s.Require().NoError(err)
		s.Len(active, 3, "all still sign until their retire_at")
		s.Equal(int32(3), active[0].Version)

		n, err = s.repo.RetireEndpointSecrets(ctx, e.ID, 3, time.Now().Add(-time.Second))
		s.Require().NoError(err)
		s.Equal(int64(2), n, "emergency: retire the others now")
		active, err = s.repo.ListActiveEndpointSecrets(ctx, e.ID)
		s.Require().NoError(err)
		s.Len(active, 1)
		s.Equal(versions[2].Version, active[0].Version)
	})
}
