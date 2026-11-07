package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	apievents "github.com/bete7512/scaffold/apps/api/events"
	"github.com/bete7512/scaffold/apps/api/models"
	"github.com/bete7512/scaffold/pkg/events"
	"github.com/bete7512/scaffold/pkg/mailer"
)

// PasswordReset sends the password reset link.
func (j *Jobs) PasswordReset(ctx context.Context, e events.Event) error {
	var p apievents.PasswordResetRequestedPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
	}
	user, err := j.deps.Users.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	token, err := j.deps.Auth.IssueActionToken(ctx, user.ID, models.TokenPurposePasswordReset, e.ID)
	if err != nil || token == "" {
		return err
	}
	link := j.deps.LinkBaseURL + "/reset-password?token=" + url.QueryEscape(token)
	return j.deps.Mail.Send(ctx, mailer.Message{
		To:      user.Email,
		Subject: "Reset your password",
		Text:    fmt.Sprintf("Hi %s,\n\nSet a new password:\n%s\n\nThe link works once and expires shortly. If you did not ask for this, ignore this email.\n", user.Name, link),
	})
}
