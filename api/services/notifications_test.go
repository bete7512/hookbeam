package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/repos/repo_mocks"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/api/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
	mailermemory "github.com/bete7512/hookbeam/pkg/mailer/memory"
)

type notificationFixture struct {
	repo     *repo_mocks.MockNotificationRepo
	prefs    *repo_mocks.MockNotificationPreferenceRepo
	outbox   *repo_mocks.MockOutboxRepo
	users    *service_mocks.MockUsers
	delivery *service_mocks.MockDeliveryService
	mail     *mailermemory.Mailer
	stored   []models.Notification
	emails   []apievents.NotificationEmailPayload
	svc      services.NotificationService
}

func newNotifications(t *testing.T, members *fakeMemberships) *notificationFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &notificationFixture{
		repo: repo_mocks.NewMockNotificationRepo(ctrl), prefs: repo_mocks.NewMockNotificationPreferenceRepo(ctrl), outbox: repo_mocks.NewMockOutboxRepo(ctrl),
		users: service_mocks.NewMockUsers(ctrl), delivery: service_mocks.NewMockDeliveryService(ctrl), mail: mailermemory.New(),
	}
	f.repo.EXPECT().WithTx(gomock.Any()).Return(f.repo).AnyTimes()
	f.outbox.EXPECT().WithTx(gomock.Any()).Return(f.outbox).AnyTimes()
	f.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
		require.Equal(t, apievents.NotificationEmailRequested, e.Type)
		var p apievents.NotificationEmailPayload
		require.NoError(t, json.Unmarshal(e.Payload, &p))
		f.emails = append(f.emails, p)
		return nil
	}).AnyTimes()
	var err error
	f.svc, err = services.NewNotificationService(services.NotificationServiceDeps{
		Tx: fakeTx{}, Notifications: f.repo, Preferences: f.prefs, Outbox: f.outbox, Memberships: members, Users: f.users, Delivery: f.delivery, Mailer: f.mail,
		LinkBaseURL: "https://console.example.com/", Logger: slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	return f
}

// storeAll makes every CreateNotification succeed and remember the row; ids count up from 100.
func (f *notificationFixture) storeAll() {
	f.repo.EXPECT().CreateNotification(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, n *models.Notification) (bool, error) {
		n.ID = int64(100 + len(f.stored))
		f.stored = append(f.stored, *n)
		return true, nil
	}).AnyTimes()
}

func TestNewNotificationService(t *testing.T) {
	_, err := services.NewNotificationService(services.NotificationServiceDeps{})
	require.Error(t, err)
}

func TestNotificationsHandleHealthChange(t *testing.T) {
	now := time.Now()
	event := apievents.EndpointHealthChangedPayload{WorkspaceID: ws, ApplicationID: 7, EndpointID: 3, EpisodeID: 9, Revision: 2, State: "unhealthy", ConsecutiveFailures: 3, OccurredAt: now}
	endpoint := &models.Endpoint{ID: 3, ApplicationID: 7, URL: "https://receiver.example.com/hooks"}
	open := &models.HealthEpisode{ID: 9, EndpointID: 3, Revision: 2, ConsecutiveFailures: 3, OpenedAt: now}
	resolvedAt := now.Add(time.Minute)
	resolved := &models.HealthEpisode{ID: 9, EndpointID: 3, Revision: 2, ConsecutiveFailures: 5, OpenedAt: now, ResolvedAt: &resolvedAt}
	team := &fakeMemberships{roles: map[string]string{owner: "owner", admin: "admin", member: "member", "u_viewer": "member"}}
	defaultPrefs := func(f *notificationFixture) {
		f.prefs.EXPECT().GetPreferences(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound)).AnyTimes()
	}

	t.Run("a failure for an open episode notifies owners and admins and asks for their emails", func(t *testing.T) {
		f := newNotifications(t, team)
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(open, endpoint, nil)
		f.storeAll()
		defaultPrefs(f)
		require.NoError(t, f.svc.HandleHealthChange(context.Background(), event))
		require.Len(t, f.stored, 2)
		recipients := map[string]bool{}
		for _, n := range f.stored {
			recipients[n.UserID] = true
			require.Equal(t, models.NotificationFailure, n.Kind)
			require.Equal(t, int64(9), n.EpisodeID)
			require.Equal(t, "Endpoint unhealthy: https://receiver.example.com/hooks", n.Title)
			require.Contains(t, n.Body, "3 consecutive attempts")
			require.Equal(t, ws, n.WorkspaceID)
		}
		require.Equal(t, map[string]bool{owner: true, admin: true}, recipients, "developers and viewers are not alerted")
		require.Len(t, f.emails, 2)
	})
	t.Run("a recipient who turned emails off still gets the notice but no email", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{roles: map[string]string{owner: "owner"}})
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(open, endpoint, nil)
		f.storeAll()
		f.prefs.EXPECT().GetPreferences(gomock.Any(), owner).Return(&models.NotificationPreferences{UserID: owner, EmailAlerts: false}, nil)
		require.NoError(t, f.svc.HandleHealthChange(context.Background(), event))
		require.Len(t, f.stored, 1)
		require.Empty(t, f.emails)
	})
	t.Run("a redelivered event finds the notices already stored and sends nothing twice", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{roles: map[string]string{owner: "owner"}})
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(open, endpoint, nil)
		f.repo.EXPECT().CreateNotification(gomock.Any(), gomock.Any()).Return(false, nil)
		require.NoError(t, f.svc.HandleHealthChange(context.Background(), event))
		require.Empty(t, f.emails)
	})
	t.Run("a late failure event for an episode that already resolved produces nothing", func(t *testing.T) {
		f := newNotifications(t, team)
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(resolved, endpoint, nil)
		require.NoError(t, f.svc.HandleHealthChange(context.Background(), event))
		require.Empty(t, f.stored)
		require.Empty(t, f.emails)
	})
	t.Run("a recovery event notifies with the recovery kind", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{roles: map[string]string{owner: "owner"}})
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(resolved, endpoint, nil)
		f.storeAll()
		defaultPrefs(f)
		recovery := event
		recovery.State = "healthy"
		require.NoError(t, f.svc.HandleHealthChange(context.Background(), recovery))
		require.Len(t, f.stored, 1)
		require.Equal(t, models.NotificationRecovery, f.stored[0].Kind)
		require.Equal(t, "Endpoint recovered: https://receiver.example.com/hooks", f.stored[0].Title)
		require.Contains(t, f.stored[0].Body, "after 5 consecutive failures")
	})
	t.Run("an incomplete or unknown event is refused", func(t *testing.T) {
		f := newNotifications(t, team)
		bad := event
		bad.State = "sleepy"
		require.ErrorIs(t, f.svc.HandleHealthChange(context.Background(), bad), apperr.ErrBadRequest)
		bad = event
		bad.EpisodeID = 0
		require.ErrorIs(t, f.svc.HandleHealthChange(context.Background(), bad), apperr.ErrBadRequest)
	})
	t.Run("a data plane failure is returned for redelivery", func(t *testing.T) {
		f := newNotifications(t, team)
		f.delivery.EXPECT().GetHealthEpisode(gomock.Any(), ws, int64(9)).Return(nil, nil, apperr.NewUnavailable("down", errors.New("dial")))
		require.ErrorIs(t, f.svc.HandleHealthChange(context.Background(), event), apperr.ErrUnavailable)
	})
}

func TestNotificationsSendEmail(t *testing.T) {
	notice := &models.Notification{ID: 12, WorkspaceID: ws, UserID: owner, Kind: models.NotificationFailure, EpisodeID: 9, EndpointID: 3, EndpointURL: "https://r", Title: "Endpoint unhealthy: https://r", Body: "3 consecutive attempts failed."}
	t.Run("a verified address gets the email with a link to the endpoint", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.repo.EXPECT().GetNotificationByID(gomock.Any(), int64(12)).Return(notice, nil)
		f.users.EXPECT().Contact(gomock.Any(), owner).Return(models.UserContact{Email: "owner@example.com", EmailVerified: true}, nil)
		require.NoError(t, f.svc.SendEmail(context.Background(), 12))
		sent := f.mail.Messages()
		require.Len(t, sent, 1)
		require.Equal(t, "owner@example.com", sent[0].To)
		require.Equal(t, notice.Title, sent[0].Subject)
		require.Contains(t, sent[0].Text, notice.Body)
		require.Contains(t, sent[0].Text, "https://console.example.com/endpoints/3")
	})
	t.Run("an unverified address is skipped without an error", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.repo.EXPECT().GetNotificationByID(gomock.Any(), int64(12)).Return(notice, nil)
		f.users.EXPECT().Contact(gomock.Any(), owner).Return(models.UserContact{Email: "owner@example.com"}, nil)
		require.NoError(t, f.svc.SendEmail(context.Background(), 12))
		require.Empty(t, f.mail.Messages())
	})
	t.Run("an unknown notification is not found", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.repo.EXPECT().GetNotificationByID(gomock.Any(), int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		require.ErrorIs(t, f.svc.SendEmail(context.Background(), -1), apperr.ErrNotFound)
	})
	t.Run("a mailer failure is returned for redelivery", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.mail.Fail = errors.New("smtp down")
		f.repo.EXPECT().GetNotificationByID(gomock.Any(), int64(12)).Return(notice, nil)
		f.users.EXPECT().Contact(gomock.Any(), owner).Return(models.UserContact{Email: "owner@example.com", EmailVerified: true}, nil)
		require.ErrorIs(t, f.svc.SendEmail(context.Background(), 12), apperr.ErrInternal)
	})
}

func TestNotificationsInbox(t *testing.T) {
	actor := services.Actor{UserID: owner, WorkspaceID: ws}
	key := services.Actor{WorkspaceID: ws}
	t.Run("list pages the actor's notices with total and unread", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.repo.EXPECT().ListNotifications(gomock.Any(), repos.ListNotificationsOpts{WorkspaceID: ws, UserID: owner, UnreadOnly: true, Limit: 20}).Return([]models.Notification{{ID: 1}}, nil)
		f.repo.EXPECT().CountNotifications(gomock.Any(), gomock.Any()).Return(1, nil)
		f.repo.EXPECT().CountUnread(gomock.Any(), ws, owner).Return(4, nil)
		items, total, unread, err := f.svc.List(context.Background(), actor, repos.ListNotificationsOpts{UnreadOnly: true, Limit: 20})
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, 1, total)
		require.Equal(t, 4, unread)

		_, _, _, err = f.svc.List(context.Background(), actor, repos.ListNotificationsOpts{Limit: 0})
		require.ErrorIs(t, err, apperr.ErrValidation)
		_, _, _, err = f.svc.List(context.Background(), key, repos.ListNotificationsOpts{Limit: 20})
		require.ErrorIs(t, err, apperr.ErrForbidden, "notices belong to people, not keys")
	})
	t.Run("mark read is scoped to the actor and idempotent", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		read := time.Now()
		f.repo.EXPECT().MarkNotificationRead(gomock.Any(), ws, owner, int64(5)).Return(&models.Notification{ID: 5, ReadAt: &read}, nil)
		n, err := f.svc.MarkRead(context.Background(), actor, 5)
		require.NoError(t, err)
		require.NotNil(t, n.ReadAt)
		f.repo.EXPECT().MarkNotificationRead(gomock.Any(), ws, owner, int64(-1)).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		_, err = f.svc.MarkRead(context.Background(), actor, -1)
		require.ErrorIs(t, err, apperr.ErrNotFound)
	})
	t.Run("preferences default to emails on and can be turned off", func(t *testing.T) {
		f := newNotifications(t, &fakeMemberships{})
		f.prefs.EXPECT().GetPreferences(gomock.Any(), owner).Return(nil, fmt.Errorf("%w: none", apperr.ErrNotFound))
		p, err := f.svc.Preferences(context.Background(), actor)
		require.NoError(t, err)
		require.True(t, p.EmailAlerts)
		f.prefs.EXPECT().SetPreferences(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, p *models.NotificationPreferences) error {
			require.Equal(t, owner, p.UserID)
			require.False(t, p.EmailAlerts)
			return nil
		})
		p, err = f.svc.SetPreferences(context.Background(), actor, false)
		require.NoError(t, err)
		require.False(t, p.EmailAlerts)
		_, err = f.svc.Preferences(context.Background(), key)
		require.ErrorIs(t, err, apperr.ErrForbidden)
	})
}
