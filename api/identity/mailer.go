package identity

import (
	"context"
	"errors"
	"strings"

	"github.com/bete7512/goauth/pkg/modules/notification"

	"github.com/bete7512/hookbeam/pkg/mailer"
)

// emailSender lets goauth's notification module send through a Mailer.
type emailSender struct {
	mailer mailer.Mailer
}

var _ notification.EmailSender = emailSender{}

// SendEmail delivers one message per recipient; the first failure stops the loop.
func (s emailSender) SendEmail(ctx context.Context, msg *notification.EmailMessage) error {
	if msg == nil || len(msg.To) == 0 {
		return errors.New("identity: message without recipients")
	}
	for _, to := range msg.To {
		m := mailer.Message{
			To:      strings.TrimSpace(to),
			Subject: msg.Subject,
			Text:    msg.TextBody,
			HTML:    msg.HTMLBody,
		}
		if err := m.Validate(); err != nil {
			return err
		}
		if err := s.mailer.Send(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// VerifyConfig reports nothing: the Mailer was validated when it was built.
func (emailSender) VerifyConfig(context.Context) error { return nil }
