package mailer

import (
	"context"
	"errors"
	"strings"

	"github.com/bete7512/goauth/pkg/modules/notification"
)

// GoAuthSender lets goauth's notification module send through a Mailer.
type GoAuthSender struct {
	mailer Mailer
}

var _ notification.EmailSender = GoAuthSender{}

// NewGoAuthSender wraps m for goauth.
func NewGoAuthSender(m Mailer) GoAuthSender { return GoAuthSender{mailer: m} }

// SendEmail delivers one message per recipient; the first failure stops the loop.
func (s GoAuthSender) SendEmail(ctx context.Context, msg *notification.EmailMessage) error {
	if msg == nil || len(msg.To) == 0 {
		return errors.New("mailer: message without recipients")
	}
	for _, to := range msg.To {
		m := Message{To: strings.TrimSpace(to), Subject: msg.Subject, Text: msg.TextBody, HTML: msg.HTMLBody}
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
func (GoAuthSender) VerifyConfig(context.Context) error { return nil }
