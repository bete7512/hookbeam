//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type SubscriptionRepoTestSuite struct {
	suite.Suite
	db        *pgxpool.Pool
	apps      repos.ApplicationRepo
	endpoints repos.EndpointRepo
	repo      repos.SubscriptionRepo
}

func TestSubscriptionRepoTestSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionRepoTestSuite))
}

func (s *SubscriptionRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.apps, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
	s.endpoints, err = repos.NewEndpointRepo(s.db)
	s.Require().NoError(err)
	s.repo, err = repos.NewSubscriptionRepo(s.db)
	s.Require().NoError(err)
}

func (s *SubscriptionRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "subscriptions", "endpoints", "applications")
}

func (s *SubscriptionRepoTestSuite) seedEndpoint() *models.Endpoint {
	app := seedApplication(&s.Suite, s.apps, "Customer A")
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor}
	s.Require().NoError(s.endpoints.CreateEndpoint(context.Background(), e))
	return e
}

func (s *SubscriptionRepoTestSuite) TestCreateSubscriptions() {
	tests := []struct {
		name    string
		types   []string
		want    []string
		wantErr error
	}{
		{name: "inserts one row per type", types: []string{"order.shipped", "order.created"}, want: []string{"order.created", "order.shipped"}},
		{name: "empty list inserts nothing", types: nil, want: []string{}},
		{name: "duplicate type conflicts", types: []string{"a", "a"}, wantErr: apperr.ErrConflict},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			e := s.seedEndpoint()
			err := s.repo.CreateSubscriptions(context.Background(), e.ID, tt.types)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			got, err := s.repo.ListEventTypes(context.Background(), e.ID)
			s.Require().NoError(err)
			s.Equal(tt.want, got)
		})
	}
	s.Run("unknown endpoint fails", func() {
		s.Error(s.repo.CreateSubscriptions(context.Background(), -1, []string{"a"}))
	})
}

func (s *SubscriptionRepoTestSuite) TestListEventTypes() {
	s.Run("unknown endpoint lists nothing", func() {
		got, err := s.repo.ListEventTypes(context.Background(), -1)
		s.Require().NoError(err)
		s.Empty(got)
	})
}
