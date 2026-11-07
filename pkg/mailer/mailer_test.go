package mailer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/mailer"
	"github.com/bete7512/hookbeam/pkg/mailer/memory"
)

func TestMessageValidate(t *testing.T) {
	tests := []struct {
		name    string
		msg     mailer.Message
		wantErr error
	}{
		{name: "missing to", msg: mailer.Message{Subject: "hi", Text: "body"}, wantErr: mailer.ErrInvalidMessage},
		{name: "missing subject", msg: mailer.Message{To: "a@example.com", Text: "body"}, wantErr: mailer.ErrInvalidMessage},
		{name: "no body", msg: mailer.Message{To: "a@example.com", Subject: "hi"}, wantErr: mailer.ErrInvalidMessage},
		{name: "text only", msg: mailer.Message{To: "a@example.com", Subject: "hi", Text: "body"}},
		{name: "html only", msg: mailer.Message{To: "a@example.com", Subject: "hi", HTML: "<p>body</p>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := memory.New()
			err := m.Send(context.Background(), tt.msg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, m.Messages())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []mailer.Message{tt.msg}, m.Messages())
		})
	}
}
