// Package jobs dispatches worker events with replay suppression and bounded processing.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/mailer"
)

// Handler processes one event.
type Handler func(ctx context.Context, e events.Event) error

// Deps are the dependencies; HandlerTimeout defaults to two minutes.
type Deps struct {
	Events         services.EventService
	Users          services.UserService
	Auth           services.AuthService
	Mail           mailer.Mailer
	LinkBaseURL    string // frontend origin the links in emails point to
	Logger         *slog.Logger
	HandlerTimeout time.Duration
}

// Jobs holds the handlers of the api app and dispatches events to them.
type Jobs struct {
	deps     Deps
	handlers map[string]Handler
}

// New returns the jobs or an error if a dependency is missing.
func New(deps Deps) (*Jobs, error) {
	if deps.HandlerTimeout == 0 {
		deps.HandlerTimeout = 2 * time.Minute
	}
	if deps.Events == nil || deps.Users == nil || deps.Auth == nil || deps.Mail == nil || deps.LinkBaseURL == "" || deps.Logger == nil || deps.HandlerTimeout <= 0 {
		return nil, errors.New("jobs: Events, Users, Auth, Mail, LinkBaseURL, Logger and positive HandlerTimeout are required")
	}
	j := &Jobs{deps: deps}
	j.handlers = map[string]Handler{
		apievents.UserRegistered:            j.Welcome,
		apievents.UserVerificationRequested: j.Verification,
		apievents.PasswordResetRequested:    j.PasswordReset,
	}
	return j, nil
}

// Handle rejects unsupported events for redelivery and DLQ routing, and suppresses recorded successful replays.
func (j *Jobs) Handle(ctx context.Context, e events.Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	h, ok := j.handlers[e.Type]
	if !ok {
		j.deps.Logger.WarnContext(ctx, "no handler for event", "type", e.Type, "event_id", e.ID)
		return fmt.Errorf("jobs: no handler for event type %q", e.Type)
	}
	ctx, cancel := context.WithTimeout(ctx, j.deps.HandlerTimeout)
	defer cancel()
	fresh, err := j.deps.Events.ProcessOnce(ctx, e.ID, func(ctx context.Context) error {
		return h(ctx, e)
	})
	if err := errors.Join(err, ctx.Err()); err != nil {
		return err
	}
	if !fresh {
		j.deps.Logger.InfoContext(ctx, "event already processed", "type", e.Type, "event_id", e.ID)
	}
	return nil
}
