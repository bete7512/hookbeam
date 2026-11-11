package identity

import (
	"context"
	"testing"

	"github.com/bete7512/goauth/pkg/modules/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/mailer/memory"
)

func TestEmailSender(t *testing.T) {
	tests := []struct {
		name    string
		msg     *notification.EmailMessage
		want    int
		wantErr bool
	}{
		{name: "one per recipient", msg: &notification.EmailMessage{To: []string{"a@example.com", " b@example.com "}, Subject: "hi", TextBody: "t"}, want: 2},
		{name: "html only is enough", msg: &notification.EmailMessage{To: []string{"a@example.com"}, Subject: "hi", HTMLBody: "<p>t</p>"}, want: 1},
		{name: "no recipients", msg: &notification.EmailMessage{Subject: "hi", TextBody: "t"}, wantErr: true},
		{name: "nil message", wantErr: true},
		{name: "no subject", msg: &notification.EmailMessage{To: []string{"a@example.com"}, TextBody: "t"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := memory.New()
			s := emailSender{mailer: box}

			err := s.SendEmail(context.Background(), tt.msg)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, box.Messages(), tt.want)
			for _, m := range box.Messages() {
				assert.NotContains(t, m.To, " ", "recipients are trimmed")
			}
			require.NoError(t, s.VerifyConfig(context.Background()))
		})
	}
}
