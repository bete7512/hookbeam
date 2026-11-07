package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	apievents "github.com/bete7512/hookbeam/api/events"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/mailer"
)

// Welcome greets a newly registered user and, unless the email is already verified, includes the verification link.
func (j *Jobs) Welcome(ctx context.Context, e events.Event) error {
	var p apievents.UserRegisteredPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("jobs: decode %s payload: %w", e.Type, err)
	}
	user, err := j.deps.Users.GetUser(ctx, p.UserID)
	if err != nil {
		return err
	}
	text := fmt.Sprintf("Hi %s,\n\nYour account is ready.\n", user.Name)
	if user.EmailVerifiedAt == nil {
		link, err := j.verificationLink(ctx, user.ID, e.ID)
		if err != nil || link == "" {
			return err
		}
		text += "\nConfirm your email address:\n" + link + "\n\nThe link works once and expires shortly.\n"
	}
	return j.deps.Mail.Send(ctx, mailer.Message{To: user.Email, Subject: "Welcome to Hookbeam", Text: text})
}

func (j *Jobs) verificationLink(ctx context.Context, userID, requestID int64) (string, error) {
	token, err := j.deps.Auth.IssueActionToken(ctx, userID, models.TokenPurposeEmailVerification, requestID)
	if err != nil || token == "" {
		return "", err
	}
	return j.deps.LinkBaseURL + "/verify-email?token=" + url.QueryEscape(token), nil
}
