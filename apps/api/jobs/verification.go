package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/mailer"
)

// Verification sends the email verification link again.
func (j *Jobs) Verification(ctx context.Context, e events.Event) error {
	var p apievents.UserVerificationRequestedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
	}
	user, err := j.deps.Users.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	if user.EmailVerifiedAt != nil {
		return nil
	}
	link, err := j.verificationLink(ctx, user.ID, e.ID)
	if err != nil || link == "" {
		return err
	}
	return j.deps.Mail.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: "Confirm your email address",
		Text:    fmt.Sprintf("Hi %s,\n\nConfirm your email address:\n%s\n\nThe link works once and expires shortly.\n", user.Name, link),
	})
}
