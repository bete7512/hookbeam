package services

//go:generate mockgen -source=notifications.go -destination=service_mocks/mock_notifications.go -package=service_mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/mailer"
)

// Users answers a user's contact details from the identity store.
type Users interface {
	Contact(ctx context.Context, userID string) (models.UserContact, error)
}

// NotificationService turns health changes into in-app notices and alert emails, and serves a user's inbox.
type NotificationService interface {
	// HandleHealthChange rereads the episode from the data plane and, unless the event is obsolete, stores one
	// notice per owner and admin and asks for an email where the recipient wants one. Repeating it is harmless.
	HandleHealthChange(ctx context.Context, p apievents.EndpointHealthChangedPayload) error
	// SendEmail mails one stored notice to its recipient when the address is verified.
	SendEmail(ctx context.Context, notificationID int64) error
	// List pages the actor's notices in the workspace, newest first, with the total and the unread count.
	List(ctx context.Context, actor Actor, opts repos.ListNotificationsOpts) (items []models.Notification, total, unread int, err error)
	MarkRead(ctx context.Context, actor Actor, id int64) (*models.Notification, error)
	Preferences(ctx context.Context, actor Actor) (*models.NotificationPreferences, error)
	SetPreferences(ctx context.Context, actor Actor, emailAlerts bool) (*models.NotificationPreferences, error)
}

// NotificationServiceDeps are the dependencies; all are required.
type NotificationServiceDeps struct {
	Tx            db.Transactor
	Notifications repos.NotificationRepo
	Preferences   repos.NotificationPreferenceRepo
	Outbox        repos.OutboxRepo
	Memberships   Memberships
	Users         Users
	Delivery      DeliveryService
	Mailer        mailer.Mailer
	LinkBaseURL   string // console origin the email links point to
	Logger        *slog.Logger
}

type notificationService struct {
	deps NotificationServiceDeps
}

// NewNotificationService returns a NotificationService or an error if a dependency is missing.
func NewNotificationService(deps NotificationServiceDeps) (NotificationService, error) {
	if deps.Tx == nil || deps.Notifications == nil || deps.Preferences == nil || deps.Outbox == nil || deps.Memberships == nil || deps.Users == nil || deps.Delivery == nil || deps.Mailer == nil || deps.LinkBaseURL == "" || deps.Logger == nil {
		return nil, errors.New("services: Tx, Notifications, Preferences, Outbox, Memberships, Users, Delivery, Mailer, LinkBaseURL and Logger are required")
	}
	deps.LinkBaseURL = strings.TrimRight(deps.LinkBaseURL, "/")
	return &notificationService{deps: deps}, nil
}

// notificationKind maps a health state to the notice it produces.
func notificationKind(state string) (string, bool) {
	switch state {
	case "unhealthy":
		return models.NotificationFailure, true
	case "healthy":
		return models.NotificationRecovery, true
	}
	return "", false
}

func (s *notificationService) HandleHealthChange(ctx context.Context, p apievents.EndpointHealthChangedPayload) error {
	kind, ok := notificationKind(p.State)
	if p.WorkspaceID == "" || p.EpisodeID <= 0 || p.EndpointID <= 0 || !ok {
		return apperr.NewBadRequest("endpoint health event is incomplete", nil)
	}
	episode, endpoint, err := s.deps.Delivery.GetHealthEpisode(ctx, p.WorkspaceID, p.EpisodeID)
	if err != nil {
		return err
	}
	if kind == models.NotificationFailure && episode.ResolvedAt != nil {
		s.deps.Logger.InfoContext(ctx, "obsolete failure event skipped", "episode_id", p.EpisodeID, "endpoint_id", p.EndpointID)
		return nil
	}
	members, err := s.deps.Memberships.Members(ctx, p.WorkspaceID)
	if err != nil {
		return apperr.NewInternal(err)
	}
	title, body := s.compose(kind, endpoint, episode)
	for _, m := range members {
		if m.Role != models.RoleOwner && m.Role != models.RoleAdmin {
			continue
		}
		if err := s.notify(ctx, &models.Notification{
			WorkspaceID: p.WorkspaceID, UserID: m.UserID, Kind: kind, EpisodeID: episode.ID, EndpointID: endpoint.ID,
			EndpointURL: endpoint.URL, ApplicationID: endpoint.ApplicationID, Title: title, Body: body,
		}); err != nil {
			return err
		}
	}
	return nil
}

// notify stores the notice and, when it is new and the recipient wants emails, asks for one in the same transaction.
func (s *notificationService) notify(ctx context.Context, n *models.Notification) error {
	err := s.deps.Tx.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		created, err := s.deps.Notifications.WithTx(tx).CreateNotification(ctx, n)
		if err != nil || !created {
			return err
		}
		wants, err := s.emailAlerts(ctx, n.UserID)
		if err != nil || !wants {
			return err
		}
		payload, err := json.Marshal(apievents.NotificationEmailPayload{NotificationID: n.ID, UserID: n.UserID})
		if err != nil {
			return err
		}
		return s.deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.NotificationEmailRequested, Payload: payload})
	})
	if err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

// emailAlerts reports the user's choice; no row means yes.
func (s *notificationService) emailAlerts(ctx context.Context, userID string) (bool, error) {
	p, err := s.deps.Preferences.GetPreferences(ctx, userID)
	if errors.Is(err, apperr.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return p.EmailAlerts, nil
}

func (s *notificationService) compose(kind string, endpoint *models.Endpoint, episode *models.HealthEpisode) (title, body string) {
	if kind == models.NotificationFailure {
		return fmt.Sprintf("Endpoint unhealthy: %s", endpoint.URL),
			fmt.Sprintf("%d consecutive attempts to %s failed since %s. Hookbeam keeps retrying on its schedule; check the receiver or pause the endpoint.", episode.ConsecutiveFailures, endpoint.URL, episode.OpenedAt.UTC().Format("2006-01-02 15:04 UTC"))
	}
	resolved := "now"
	if episode.ResolvedAt != nil {
		resolved = episode.ResolvedAt.UTC().Format("2006-01-02 15:04 UTC")
	}
	return fmt.Sprintf("Endpoint recovered: %s", endpoint.URL),
		fmt.Sprintf("Deliveries to %s succeed again as of %s, after %d consecutive failures.", endpoint.URL, resolved, episode.ConsecutiveFailures)
}

func (s *notificationService) SendEmail(ctx context.Context, notificationID int64) error {
	n, err := s.deps.Notifications.GetNotificationByID(ctx, notificationID)
	if err != nil {
		return notificationError(err)
	}
	contact, err := s.deps.Users.Contact(ctx, n.UserID)
	if err != nil {
		return notificationError(err)
	}
	if !contact.EmailVerified {
		s.deps.Logger.InfoContext(ctx, "alert email skipped, address not verified", "notification_id", n.ID, "user_id", n.UserID)
		return nil
	}
	msg := mailer.Message{
		To:      contact.Email,
		Subject: n.Title,
		Text:    fmt.Sprintf("%s\n\nEndpoint: %s/endpoints/%d\n", n.Body, s.deps.LinkBaseURL, n.EndpointID),
	}
	if err := s.deps.Mailer.Send(ctx, msg); err != nil {
		return apperr.NewInternal(err)
	}
	return nil
}

func (s *notificationService) List(ctx context.Context, actor Actor, opts repos.ListNotificationsOpts) ([]models.Notification, int, int, error) {
	if err := requireUser(actor); err != nil {
		return nil, 0, 0, err
	}
	if opts.Limit < 1 || opts.Limit > 100 || opts.Offset < 0 {
		return nil, 0, 0, apperr.NewValidation(map[string]string{"limit": "must be between 1 and 100 with a non-negative offset"})
	}
	opts.WorkspaceID, opts.UserID = actor.WorkspaceID, actor.UserID
	items, err := s.deps.Notifications.ListNotifications(ctx, opts)
	if err != nil {
		return nil, 0, 0, apperr.NewInternal(err)
	}
	total, err := s.deps.Notifications.CountNotifications(ctx, opts)
	if err != nil {
		return nil, 0, 0, apperr.NewInternal(err)
	}
	unread, err := s.deps.Notifications.CountUnread(ctx, actor.WorkspaceID, actor.UserID)
	if err != nil {
		return nil, 0, 0, apperr.NewInternal(err)
	}
	return items, total, unread, nil
}

func (s *notificationService) MarkRead(ctx context.Context, actor Actor, id int64) (*models.Notification, error) {
	if err := requireUser(actor); err != nil {
		return nil, err
	}
	n, err := s.deps.Notifications.MarkNotificationRead(ctx, actor.WorkspaceID, actor.UserID, id)
	if err != nil {
		return nil, notificationError(err)
	}
	return n, nil
}

func (s *notificationService) Preferences(ctx context.Context, actor Actor) (*models.NotificationPreferences, error) {
	if actor.UserID == "" {
		return nil, apperr.NewForbidden("a user session is required", nil)
	}
	p, err := s.deps.Preferences.GetPreferences(ctx, actor.UserID)
	if errors.Is(err, apperr.ErrNotFound) {
		return &models.NotificationPreferences{UserID: actor.UserID, EmailAlerts: true}, nil
	}
	if err != nil {
		return nil, apperr.NewInternal(err)
	}
	return p, nil
}

func (s *notificationService) SetPreferences(ctx context.Context, actor Actor, emailAlerts bool) (*models.NotificationPreferences, error) {
	if actor.UserID == "" {
		return nil, apperr.NewForbidden("a user session is required", nil)
	}
	p := &models.NotificationPreferences{UserID: actor.UserID, EmailAlerts: emailAlerts}
	if err := s.deps.Preferences.SetPreferences(ctx, p); err != nil {
		return nil, apperr.NewInternal(err)
	}
	return p, nil
}

// requireUser checks the actor is a user acting in a workspace: notices belong to a person, never to a key.
func requireUser(actor Actor) error {
	if actor.UserID == "" {
		return apperr.NewForbidden("a user session is required", nil)
	}
	if actor.WorkspaceID == "" {
		return apperr.NewForbidden("select a workspace", nil)
	}
	return nil
}

func notificationError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("notification", err)
	}
	return apperr.NewInternal(err)
}
