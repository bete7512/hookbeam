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
	testutil.Truncate(s.T(), s.db, "endpoints", "applications", "environments")
}

// seedEndpoint creates an application and an endpoint under it.
func (s *EndpointRepoTestSuite) seedEndpoint() *models.Endpoint {
	app := seedApplication(&s.Suite, s.db, s.apps, "Customer A")
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

func (s *EndpointRepoTestSuite) TestResumeEndpoint() {
	ctx := context.Background()
	tests := []struct {
		name    string
		prepare func(e *models.Endpoint)
		wantErr error
	}{
		{name: "resumes a paused endpoint", prepare: func(e *models.Endpoint) {
			_, err := s.repo.PauseEndpoint(ctx, ws, e.ID)
			s.Require().NoError(err)
		}},
		{name: "not paused is not found", wantErr: apperr.ErrNotFound},
		{name: "archived is not found", prepare: func(e *models.Endpoint) {
			_, err := s.repo.PauseEndpoint(ctx, ws, e.ID)
			s.Require().NoError(err)
			_, err = s.repo.ArchiveEndpoint(ctx, ws, e.ID)
			s.Require().NoError(err)
		}, wantErr: apperr.ErrNotFound},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			e := s.seedEndpoint()
			if tt.prepare != nil {
				tt.prepare(e)
			}
			got, err := s.repo.ResumeEndpoint(ctx, ws, e.ID)
			if tt.wantErr != nil {
				s.Require().ErrorIs(err, tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Nil(got.PausedAt)
		})
	}
}

func (s *EndpointRepoTestSuite) TestArchiveEndpoint() {
	ctx := context.Background()
	s.Run("archives once", func() {
		e := s.seedEndpoint()
		got, err := s.repo.ArchiveEndpoint(ctx, ws, e.ID)
		s.Require().NoError(err)
		s.NotNil(got.ArchivedAt)
		_, err = s.repo.ArchiveEndpoint(ctx, ws, e.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		_, err = s.repo.PauseEndpoint(ctx, ws, e.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound, "an archived endpoint cannot be paused")
	})
	s.Run("another workspace is not found", func() {
		e := s.seedEndpoint()
		_, err := s.repo.ArchiveEndpoint(ctx, "ws_other", e.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}

func (s *EndpointRepoTestSuite) TestUpdateEndpoint() {
	ctx := context.Background()
	s.Run("bumps the revision on every change", func() {
		e := s.seedEndpoint()
		got, err := s.repo.UpdateEndpoint(ctx, ws, e.ID, "https://new.example.com/hooks", "changed", nil)
		s.Require().NoError(err)
		s.Equal(int32(2), got.Revision)
		s.Equal("https://new.example.com/hooks", got.URL)
		got, err = s.repo.UpdateEndpoint(ctx, ws, e.ID, got.URL, "changed again", nil)
		s.Require().NoError(err)
		s.Equal(int32(3), got.Revision)
	})
	s.Run("archived or foreign is not found", func() {
		e := s.seedEndpoint()
		_, err := s.repo.UpdateEndpoint(ctx, "ws_other", e.ID, "https://x.example.com", "", nil)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		_, err = s.repo.ArchiveEndpoint(ctx, ws, e.ID)
		s.Require().NoError(err)
		_, err = s.repo.UpdateEndpoint(ctx, ws, e.ID, "https://x.example.com", "", nil)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
}

func (s *EndpointRepoTestSuite) TestEndpointCounts() {
	s.Run("active endpoints per application; archived ones do not count", func() {
		ctx := context.Background()
		first := s.seedEndpoint()
		second := &models.Endpoint{WorkspaceID: ws, ApplicationID: first.ApplicationID, URL: "https://two.example.com/hooks", CreatedBy: actor}
		s.Require().NoError(s.repo.CreateEndpoint(ctx, second))
		n, err := s.repo.CountActiveEndpoints(ctx, first.ApplicationID)
		s.Require().NoError(err)
		s.Equal(2, n)
		_, err = s.repo.ArchiveEndpoint(ctx, ws, second.ID)
		s.Require().NoError(err)
		n, err = s.repo.CountActiveEndpoints(ctx, first.ApplicationID)
		s.Require().NoError(err)
		s.Equal(1, n)
		n, err = s.repo.CountActiveEndpoints(ctx, -1)
		s.Require().NoError(err)
		s.Zero(n)

		counts, err := s.repo.ListEndpointCounts(ctx, ws)
		s.Require().NoError(err)
		s.Require().Len(counts, 1)
		s.Equal(first.ApplicationID, counts[0].ApplicationID)
		s.Equal("Customer A", counts[0].Name)
		s.Equal(int32(1), counts[0].Endpoints)
		counts, err = s.repo.ListEndpointCounts(ctx, "ws_other")
		s.Require().NoError(err)
		s.Empty(counts)
	})
}

func (s *EndpointRepoTestSuite) TestListEndpointsAndExpectedRevision() {
	ctx := context.Background()
	s.Run("lists the application's endpoints, archived included, scoped by workspace", func() {
		first := s.seedEndpoint()
		second := &models.Endpoint{WorkspaceID: ws, ApplicationID: first.ApplicationID, URL: "https://two.example.com/hooks", CreatedBy: actor}
		s.Require().NoError(s.repo.CreateEndpoint(ctx, second))
		_, err := s.repo.ArchiveEndpoint(ctx, ws, second.ID)
		s.Require().NoError(err)
		list, err := s.repo.ListEndpoints(ctx, ws, first.ApplicationID)
		s.Require().NoError(err)
		s.Require().Len(list, 2)
		s.Nil(list[0].ArchivedAt)
		s.NotNil(list[1].ArchivedAt)
		list, err = s.repo.ListEndpoints(ctx, "ws_other", first.ApplicationID)
		s.Require().NoError(err)
		s.Empty(list)

		subs, err := repos.NewSubscriptionRepo(s.db)
		s.Require().NoError(err)
		s.Require().NoError(subs.CreateSubscriptions(ctx, second.ID, []string{"b", "a"}))
		types, err := subs.ListEventTypesForEndpoints(ctx, []int64{first.ID, second.ID})
		s.Require().NoError(err)
		s.NotContains(types, first.ID, "no subscription rows: every type")
		s.Equal([]string{"a", "b"}, types[second.ID])
		types, err = subs.ListEventTypesForEndpoints(ctx, nil)
		s.Require().NoError(err)
		s.Empty(types)
	})
	s.Run("an update conditional on the revision applies once", func() {
		e := s.seedEndpoint()
		one := int32(1)
		got, err := s.repo.UpdateEndpoint(ctx, ws, e.ID, e.URL, "first", &one)
		s.Require().NoError(err)
		s.Equal(int32(2), got.Revision)
		_, err = s.repo.UpdateEndpoint(ctx, ws, e.ID, e.URL, "second", &one)
		s.Require().ErrorIs(err, apperr.ErrNotFound, "the row moved on")
		got, err = s.repo.UpdateEndpoint(ctx, ws, e.ID, e.URL, "third", nil)
		s.Require().NoError(err)
		s.Equal(int32(3), got.Revision, "no expectation, no condition")
	})
}
