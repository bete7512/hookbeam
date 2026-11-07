// Package smtp delivers mail over SMTP, with STARTTLS when offered and PLAIN auth when configured.
package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"time"

	"github.com/bete7512/hookbeam/pkg/mailer"
)

// Config locates the server and the sender; Username empty means no auth.
type Config struct {
	Addr     string
	From     string
	Username string
	Password string
}

// Mailer sends each message over its own SMTP session.
type Mailer struct {
	cfg Config
}

// New returns a Mailer for cfg.
func New(cfg Config) *Mailer { return &Mailer{cfg: cfg} }

// Send builds the RFC 5322 message and delivers it; ctx bounds the whole session.
func (m *Mailer) Send(ctx context.Context, msg mailer.Message) error {
	if err := msg.Validate(); err != nil {
		return err
	}
	from, err := mail.ParseAddress(m.cfg.From)
	if err != nil {
		return fmt.Errorf("smtp: from address: %w", err)
	}
	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("smtp: to address: %w", err)
	}
	body, err := build(from, to, msg, time.Now())
	if err != nil {
		return err
	}
	if err := m.deliver(ctx, from.Address, to.Address, body); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("smtp: %w", ctx.Err())
		}
		// Socket deadlines may fire before context's timer goroutine records its error.
		var timeout net.Error
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) && errors.As(err, &timeout) && timeout.Timeout() {
			return fmt.Errorf("smtp: %w", context.DeadlineExceeded)
		}
		return fmt.Errorf("smtp: %w", err)
	}
	return nil
}

func (m *Mailer) deliver(ctx context.Context, from, to string, body []byte) error {
	host, _, err := net.SplitHostPort(m.cfg.Addr)
	if err != nil {
		return err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.cfg.Addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
