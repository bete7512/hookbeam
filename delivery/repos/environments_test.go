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

type EnvironmentRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.EnvironmentRepo
	apps repos.ApplicationRepo
}

func TestEnvironmentRepoTestSuite(t *testing.T) {
	suite.Run(t, new(EnvironmentRepoTestSuite))
}

func (s *EnvironmentRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewEnvironmentRepo(s.db)
	s.Require().NoError(err)
	s.apps, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
}

func (s *EnvironmentRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "applications", "environments")
}

func (s *EnvironmentRepoTestSuite) TestEnvironments() {
	ctx := context.Background()
	s.Run("create, get, list, scoped by workspace", func() {
		prod := &models.Environment{WorkspaceID: ws, Name: "production", Kind: models.EnvironmentProduction, CreatedBy: actor}
		s.Require().NoError(s.repo.CreateEnvironment(ctx, prod))
		s.Positive(prod.ID)
		got, err := s.repo.GetEnvironment(ctx, ws, prod.ID)
		s.Require().NoError(err)
		s.Equal("production", got.Name)
		_, err = s.repo.GetEnvironment(ctx, "ws_other", prod.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		dup := &models.Environment{WorkspaceID: ws, Name: "production", Kind: models.EnvironmentProduction, CreatedBy: actor}
		s.Require().ErrorIs(s.repo.CreateEnvironment(ctx, dup), apperr.ErrConflict)
		s.Require().Error(s.repo.CreateEnvironment(ctx, &models.Environment{WorkspaceID: ws, Name: "staging", Kind: "staging", CreatedBy: actor}), "kind is checked")
		list, err := s.repo.ListEnvironments(ctx, ws)
		s.Require().NoError(err)
		s.Len(list, 1)
		other, err := s.repo.ListEnvironments(ctx, "ws_other")
		s.Require().NoError(err)
		s.Empty(other)
	})
	s.Run("the default development environment is created once", func() {
		first, err := s.repo.EnsureDefaultEnvironment(ctx, ws, actor)
		s.Require().NoError(err)
		second, err := s.repo.EnsureDefaultEnvironment(ctx, ws, "u_other")
		s.Require().NoError(err)
		s.Equal(first.ID, second.ID)
		s.Equal(models.EnvironmentDevelopment, first.Kind)
	})
	s.Run("application names are unique per environment and reads carry the kind", func() {
		dev, err := s.repo.EnsureDefaultEnvironment(ctx, ws, actor)
		s.Require().NoError(err)
		prod := &models.Environment{WorkspaceID: ws, Name: "production", Kind: models.EnvironmentProduction, CreatedBy: actor}
		s.Require().NoError(s.repo.CreateEnvironment(ctx, prod))
		a := &models.Application{WorkspaceID: ws, EnvironmentID: dev.ID, Name: "Customer A", CreatedBy: actor}
		s.Require().NoError(s.apps.CreateApplication(ctx, a))
		s.Equal(models.EnvironmentDevelopment, a.EnvironmentKind, "the insert returns the kind")
		b := &models.Application{WorkspaceID: ws, EnvironmentID: prod.ID, Name: "Customer A", CreatedBy: actor}
		s.Require().NoError(s.apps.CreateApplication(ctx, b), "same name in another environment")
		got, err := s.apps.GetApplication(ctx, ws, b.ID)
		s.Require().NoError(err)
		s.Equal(models.EnvironmentProduction, got.EnvironmentKind)
		s.Require().ErrorIs(s.apps.CreateApplication(ctx, &models.Application{WorkspaceID: ws, EnvironmentID: prod.ID, Name: "Customer A", CreatedBy: actor}), apperr.ErrConflict)
	})
}
