// Package backend picks the mailer a process sends through from its configuration.
package backend

import (
	"context"
	"fmt"

	"github.com/bete7512/hookbeam/pkg/mailer"
	"github.com/bete7512/hookbeam/pkg/mailer/memory"
	"github.com/bete7512/hookbeam/pkg/mailer/ses"
	"github.com/bete7512/hookbeam/pkg/mailer/smtp"
)

// Config names the backend and what it needs: memory nothing, smtp an address, ses a region.
type Config struct {
	Backend  string // memory | smtp | ses
	SMTPAddr string
	From     string
	Region   string
}

// New returns the configured Mailer.
func New(ctx context.Context, cfg Config) (mailer.Mailer, error) {
	switch cfg.Backend {
	case "memory":
		return memory.New(), nil
	case "smtp":
		return smtp.New(smtp.Config{Addr: cfg.SMTPAddr, From: cfg.From}), nil
	case "ses":
		client, err := ses.NewClient(ctx, cfg.Region)
		if err != nil {
			return nil, err
		}
		return ses.New(client, cfg.From), nil
	default:
		return nil, fmt.Errorf("mailer: unknown backend %q", cfg.Backend)
	}
}
