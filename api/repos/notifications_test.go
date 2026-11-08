//go:build integration

package repos_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/api/migrations"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/testutil"
)

type NotificationRepoTestSuite struct {
	suite.Suite
	db    *pgxpool.Pool
	repo  repos.NotificationRepo
	prefs repos.NotificationPreferenceRepo
}

func TestNotificationRepoTestSuite(t *testing.T) {
	suite.Run(t, new(NotificationRepoTestSuite))
}

func (s *NotificationRepoTestSuite) SetupSuite() {
	s.db = testutil.Postgres(s.T(), migrations.Up)
	var err error
	s.repo, err = repos.NewNotificationRepo(s.db)
	s.Require().NoError(err)
	s.prefs, err = repos.NewNotificationPreferenceRepo(s.db)
	s.Require().NoError(err)
}

func (s *NotificationRepoTestSuite) SetupSubTest() {
	testutil.Truncate(s.T(), s.db, "notifications", "notification_preferences")
}

func (s *NotificationRepoTestSuite) notice(user, kind string, episode int64) *models.Notification {
	return &models.Notification{WorkspaceID: "ws_1", UserID: user, Kind: kind, EpisodeID: episode, EndpointID: 3, EndpointURL: "https://r", ApplicationID: 7, Title: "t", Body: "b"}
}

func (s *NotificationRepoTestSuite) TestNotifications() {
	ctx := context.Background()
	s.Run("one notice per episode, kind and recipient; list newest first; unread filter and count; scoped mark read", func() {
		created, err := s.repo.CreateNotification(ctx, s.notice("u_ada", models.NotificationFailure, 9))
		s.Require().NoError(err)
		s.True(created)
		again := s.notice("u_ada", models.NotificationFailure, 9)
		created, err = s.repo.CreateNotification(ctx, again)
		s.Require().NoError(err)
		s.False(created, "the same episode, kind and recipient is written once")
		s.Zero(again.ID)
		recovery := s.notice("u_ada", models.NotificationRecovery, 9)
		created, err = s.repo.CreateNotification(ctx, recovery)
		s.Require().NoError(err)
		s.True(created, "a different kind for the same episode is a new notice")
		created, err = s.repo.CreateNotification(ctx, s.notice("u_bob", models.NotificationFailure, 9))
		s.Require().NoError(err)
		s.True(created)

		list, err := s.repo.ListNotifications(ctx, repos.ListNotificationsOpts{WorkspaceID: "ws_1", UserID: "u_ada", Limit: 10})
		s.Require().NoError(err)
		s.Require().Len(list, 2)
		s.Equal(recovery.ID, list[0].ID, "newest first")
		total, err := s.repo.CountNotifications(ctx, repos.ListNotificationsOpts{WorkspaceID: "ws_1", UserID: "u_ada"})
		s.Require().NoError(err)
		s.Equal(2, total)
		unread, err := s.repo.CountUnread(ctx, "ws_1", "u_ada")
		s.Require().NoError(err)
		s.Equal(2, unread)

		read, err := s.repo.MarkNotificationRead(ctx, "ws_1", "u_ada", recovery.ID)
		s.Require().NoError(err)
		s.Require().NotNil(read.ReadAt)
		first := *read.ReadAt
		read, err = s.repo.MarkNotificationRead(ctx, "ws_1", "u_ada", recovery.ID)
		s.Require().NoError(err)
		s.Equal(first, *read.ReadAt, "reading again keeps the first time")
		_, err = s.repo.MarkNotificationRead(ctx, "ws_1", "u_bob", recovery.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound, "another user's notice is not visible")
		_, err = s.repo.MarkNotificationRead(ctx, "ws_other", "u_ada", recovery.ID)
		s.Require().ErrorIs(err, apperr.ErrNotFound)

		onlyUnread, err := s.repo.ListNotifications(ctx, repos.ListNotificationsOpts{WorkspaceID: "ws_1", UserID: "u_ada", UnreadOnly: true, Limit: 10})
		s.Require().NoError(err)
		s.Require().Len(onlyUnread, 1)
		s.Equal(models.NotificationFailure, onlyUnread[0].Kind)
		unread, err = s.repo.CountUnread(ctx, "ws_1", "u_ada")
		s.Require().NoError(err)
		s.Equal(1, unread)

		got, err := s.repo.GetNotificationByID(ctx, recovery.ID)
		s.Require().NoError(err)
		s.Equal("u_ada", got.UserID)
		_, err = s.repo.GetNotificationByID(ctx, -1)
		s.Require().ErrorIs(err, apperr.ErrNotFound)
	})
	s.Run("preferences: missing row is not found, set upserts", func() {
		_, err := s.prefs.GetPreferences(ctx, "u_ada")
		s.Require().ErrorIs(err, apperr.ErrNotFound)
		p := &models.NotificationPreferences{UserID: "u_ada", EmailAlerts: false}
		s.Require().NoError(s.prefs.SetPreferences(ctx, p))
		s.False(p.UpdatedAt.IsZero())
		got, err := s.prefs.GetPreferences(ctx, "u_ada")
		s.Require().NoError(err)
		s.False(got.EmailAlerts)
		p.EmailAlerts = true
		s.Require().NoError(s.prefs.SetPreferences(ctx, p))
		got, err = s.prefs.GetPreferences(ctx, "u_ada")
		s.Require().NoError(err)
		s.True(got.EmailAlerts)
	})
}
