//go:build integration

package repos_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/migrations"
	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

const (
	ws    = "ws_1"
	actor = "u_ada"
)

// seedApplication inserts an application in ws and returns it.
// seedApplication creates an application in the workspace's development environment, creating that first.
func seedApplication(s *suite.Suite, conn *pgxpool.Pool, repo repos.ApplicationRepo, name string) *models.Application {
	s.T().Helper()
	envs, err := repos.NewEnvironmentRepo(conn)
	s.Require().NoError(err)
	env, err := envs.EnsureDefaultEnvironment(context.Background(), ws, actor)
	s.Require().NoError(err)
	a := &models.Application{WorkspaceID: ws, EnvironmentID: env.ID, Name: name, CreatedBy: actor}
	s.Require().NoError(repo.CreateApplication(context.Background(), a))
	return a
}

type ApplicationRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	repo repos.ApplicationRepo
}

func TestApplicationRepoTestSuite(t *testing.T) {
	suite.Run(t, new(ApplicationRepoTestSuite))
}

func (s *ApplicationRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
}

func (s *ApplicationRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "applications", "environments")
}

func (s *ApplicationRepoTestSuite) TestCreateApplication() {
	tests := []struct {
		name    string
		seed    func()
		app     models.Application
		wantErr error
	}{
		{name: "creates and fills generated fields", app: models.Application{WorkspaceID: ws, Name: "Customer A", CreatedBy: actor}},
		{name: "same name in another workspace is fine", seed: func() { seedApplication(&s.Suite, s.db, s.repo, "Customer A") }, app: models.Application{WorkspaceID: "ws_2", Name: "Customer A", CreatedBy: actor}},
		{name: "duplicate name in the workspace conflicts", seed: func() { seedApplication(&s.Suite, s.db, s.repo, "Customer A") }, app: models.Application{WorkspaceID: ws, Name: "Customer A", CreatedBy: actor}, wantErr: apperr.ErrConflict},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			if tt.seed != nil {
				tt.seed()
			}
			a := tt.app
			envs, err := repos.NewEnvironmentRepo(s.db)
			s.Require().NoError(err)
			env, err := envs.EnsureDefaultEnvironment(context.Background(), a.WorkspaceID, actor)
			s.Require().NoError(err)
			a.EnvironmentID = env.ID
			err = s.repo.CreateApplication(context.Background(), &a)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Positive(a.ID)
			s.False(a.CreatedAt.IsZero())
			s.Nil(a.ArchivedAt)
			s.Equal(models.EnvironmentDevelopment, a.EnvironmentKind)
		})
	}
}

func (s *ApplicationRepoTestSuite) TestGetApplication() {
	tests := []struct {
		name      string
		workspace string
		id        func(seeded *models.Application) int64
		wantErr   error
	}{
		{name: "found in its workspace", workspace: ws, id: func(a *models.Application) int64 { return a.ID }},
		{name: "not visible from another workspace", workspace: "ws_2", id: func(a *models.Application) int64 { return a.ID }, wantErr: apperr.ErrNotFound},
		{name: "unknown id", workspace: ws, id: func(*models.Application) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			seeded := seedApplication(&s.Suite, s.db, s.repo, "Customer A")
			got, err := s.repo.GetApplication(context.Background(), tt.workspace, tt.id(seeded))
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(seeded.ID, got.ID)
			s.Equal("Customer A", got.Name)
		})
	}
}

func (s *ApplicationRepoTestSuite) TestSetResponsePreviews() {
	tests := []struct {
		name      string
		workspace string
		id        func(seeded *models.Application) int64
		wantErr   error
	}{
		{name: "flips the setting in its workspace", workspace: ws, id: func(a *models.Application) int64 { return a.ID }},
		{name: "not visible from another workspace", workspace: "ws_2", id: func(a *models.Application) int64 { return a.ID }, wantErr: apperr.ErrNotFound},
		{name: "unknown id", workspace: ws, id: func(*models.Application) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			seeded := seedApplication(&s.Suite, s.db, s.repo, "Customer A")
			s.False(seeded.ResponsePreviewsEnabled, "off by default")
			got, err := s.repo.SetResponsePreviews(context.Background(), tt.workspace, tt.id(seeded), true)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.True(got.ResponsePreviewsEnabled)
			s.Equal(models.EnvironmentDevelopment, got.EnvironmentKind)
			again, err := s.repo.GetApplication(context.Background(), ws, seeded.ID)
			s.Require().NoError(err)
			s.True(again.ResponsePreviewsEnabled)
		})
	}
}
