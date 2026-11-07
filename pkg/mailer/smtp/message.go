package smtp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/bete7512/hookbeam/pkg/mailer"
)

// build renders msg as an RFC 5322 message with quoted-printable UTF-8 bodies.
func build(from, to *mail.Address, msg mailer.Message, now time.Time) ([]byte, error) {
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", messageID(from.Address))
	header("MIME-Version", "1.0")

	if msg.Text == "" || msg.HTML == "" {
		ctype, body := "text/plain", msg.Text
		if msg.Text == "" {
			ctype, body = "text/html", msg.HTML
		}
		header("Content-Type", ctype+"; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, body); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}

	mw := multipart.NewWriter(&b)
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	if err := part(mw, "text/plain", msg.Text); err != nil {
		return nil, err
	}
	if err := part(mw, "text/html", msg.HTML); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func part(mw *multipart.Writer, ctype, body string) error {
	w, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {ctype + "; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	return writeQP(w, body)
}

func writeQP(w io.Writer, body string) error {
	q := quotedprintable.NewWriter(w)
	if _, err := io.WriteString(q, body); err != nil {
		return err
	}
	return q.Close()
}

func messageID(from string) string {
	domain := "localhost"
	if _, d, ok := strings.Cut(from, "@"); ok && d != "" {
		domain = d
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	return "<" + hex.EncodeToString(id[:]) + "@" + domain + ">"
}
