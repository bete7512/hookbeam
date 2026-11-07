package smtp_test

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/mailer"
	"github.com/bete7512/hookbeam/pkg/mailer/smtp"
)

func TestSend(t *testing.T) {
	tests := []struct {
		name      string
		msg       mailer.Message
		wantParts map[string]string // content type → decoded body
	}{
		{
			name:      "text only",
			msg:       mailer.Message{To: "ada@example.com", Subject: "Hello Ada", Text: "plain body"},
			wantParts: map[string]string{"text/plain": "plain body"},
		},
		{
			name:      "html only",
			msg:       mailer.Message{To: "ada@example.com", Subject: "Hello Ada", HTML: "<p>html body</p>"},
			wantParts: map[string]string{"text/html": "<p>html body</p>"},
		},
		{
			name:      "text and html",
			msg:       mailer.Message{To: "ada@example.com", Subject: "Hello Ada", Text: "plain body", HTML: "<p>html body</p>"},
			wantParts: map[string]string{"text/plain": "plain body", "text/html": "<p>html body</p>"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := startFake(t)
			m := smtp.New(smtp.Config{Addr: srv.addr, From: "Scaffold <noreply@example.com>"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			require.NoError(t, m.Send(ctx, tt.msg))

			raw := <-srv.data
			msg, err := mail.ReadMessage(strings.NewReader(raw))
			require.NoError(t, err)
			assert.Equal(t, "\"Scaffold\" <noreply@example.com>", msg.Header.Get("From"))
			assert.Equal(t, "<ada@example.com>", msg.Header.Get("To"))
			assert.Equal(t, "Hello Ada", msg.Header.Get("Subject"))
			assert.Equal(t, "1.0", msg.Header.Get("MIME-Version"))
			assert.Regexp(t, `^<[0-9a-f]{32}@example\.com>$`, msg.Header.Get("Message-ID"))
			_, err = msg.Header.Date()
			require.NoError(t, err)

			assert.Equal(t, tt.wantParts, parts(t, msg))
		})
	}
}

func TestSendContextTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	m := smtp.New(smtp.Config{Addr: ln.Addr().String(), From: "noreply@example.com"})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = m.Send(ctx, mailer.Message{To: "ada@example.com", Subject: "hi", Text: "body"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSendInvalidMessage(t *testing.T) {
	m := smtp.New(smtp.Config{Addr: "127.0.0.1:1", From: "noreply@example.com"})
	err := m.Send(context.Background(), mailer.Message{To: "ada@example.com"})
	require.ErrorIs(t, err, mailer.ErrInvalidMessage)
}

// parts decodes the message body into content type → text, expanding multipart/alternative.
func parts(t *testing.T, msg *mail.Message) map[string]string {
	t.Helper()
	got := map[string]string{}
	ctype, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	if ctype != "multipart/alternative" {
		assert.Equal(t, "utf-8", params["charset"])
		assert.Equal(t, "quoted-printable", msg.Header.Get("Content-Transfer-Encoding"))
		got[ctype] = decodeQP(t, msg.Body)
		return got
	}
	require.NotEmpty(t, params["boundary"])
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := mr.NextRawPart()
		if errors.Is(err, io.EOF) {
			return got
		}
		require.NoError(t, err)
		pctype, pparams, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
		require.NoError(t, err)
		assert.Equal(t, "utf-8", pparams["charset"])
		assert.Equal(t, "quoted-printable", p.Header.Get("Content-Transfer-Encoding"))
		got[pctype] = decodeQP(t, p)
	}
}

func decodeQP(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(quotedprintable.NewReader(r))
	require.NoError(t, err)
	return strings.TrimSuffix(string(b), "\r\n")
}
