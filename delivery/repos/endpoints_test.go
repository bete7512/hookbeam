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

type EndpointRepoTestSuite struct {
	suite.Suite
	db   *pgxpool.Pool
	apps repos.ApplicationRepo
	repo repos.EndpointRepo
}

func TestEndpointRepoTestSuite(t *testing.T) {
	suite.Run(t, new(EndpointRepoTestSuite))
}

func (s *EndpointRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.apps, err = repos.NewApplicationRepo(s.db)
	s.Require().NoError(err)
	s.repo, err = repos.NewEndpointRepo(s.db)
	s.Require().NoError(err)
}

func (s *EndpointRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "endpoints", "applications")
}

// seedEndpoint creates an application and an endpoint under it.
func (s *EndpointRepoTestSuite) seedEndpoint() *models.Endpoint {
	app := seedApplication(&s.Suite, s.apps, "Customer A")
	e := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://receiver.example.com/hooks", CreatedBy: actor, EventTypes: []string{"a"}}
	s.Require().NoError(s.repo.CreateEndpoint(context.Background(), e))
	return e
}

func (s *EndpointRepoTestSuite) TestCreateEndpoint() {
	s.Run("creates with revision 1 and keeps the event types it was given", func() {
		e := s.seedEndpoint()
		s.Positive(e.ID)
		s.Equal(int32(1), e.Revision)
		s.Empty(e.Description)
		s.Nil(e.PausedAt)
		s.Equal([]string{"a"}, e.EventTypes)
	})
	s.Run("unknown application fails", func() {
		e := &models.Endpoint{WorkspaceID: ws, ApplicationID: -1, URL: "https://x", CreatedBy: actor}
		s.Error(s.repo.CreateEndpoint(context.Background(), e))
	})
}

func (s *EndpointRepoTestSuite) TestGetEndpoint() {
	tests := []struct {
		name      string
		workspace string
		id        func(seeded *models.Endpoint) int64
		wantErr   error
	}{
		{name: "found", workspace: ws, id: func(e *models.Endpoint) int64 { return e.ID }},
		{name: "other workspace", workspace: "ws_2", id: func(e *models.Endpoint) int64 { return e.ID }, wantErr: apperr.ErrNotFound},
		{name: "unknown", workspace: ws, id: func(*models.Endpoint) int64 { return -1 }, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			seeded := s.seedEndpoint()
			got, err := s.repo.GetEndpoint(context.Background(), tt.workspace, tt.id(seeded))
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(seeded.URL, got.URL)
			s.Empty(got.EventTypes, "event types are the subscription repo's")
		})
	}
}

func (s *EndpointRepoTestSuite) TestPauseEndpoint() {
	tests := []struct {
		name    string
		prepare func(e *models.Endpoint)
		wantErr error
	}{
		{name: "pauses an active endpoint"},
		{name: "already paused is not found", prepare: func(e *models.Endpoint) {
			_, err := s.repo.PauseEndpoint(context.Background(), ws, e.ID)
			s.Require().NoError(err)
		}, wantErr: apperr.ErrNotFound},
		{name: "archived is not found", prepare: func(e *models.Endpoint) {
			_, err := s.db.Exec(context.Background(), `UPDATE endpoints SET archived_at = now() WHERE id = $1`, e.ID)
			s.Require().NoError(err)
		}, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			e := s.seedEndpoint()
			if tt.prepare != nil {
				tt.prepare(e)
			}
			got, err := s.repo.PauseEndpoint(context.Background(), ws, e.ID)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.NotNil(got.PausedAt)
			s.Equal(int32(1), got.Revision, "pause is not a configuration change")
			s.True(got.UpdatedAt.After(e.UpdatedAt), "trigger touched updated_at")
		})
	}
	s.Run("other workspace", func() {
		e := s.seedEndpoint()
		_, err := s.repo.PauseEndpoint(context.Background(), "ws_2", e.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}
