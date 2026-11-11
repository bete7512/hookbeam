package backend_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/mailer/backend"
	"github.com/bete7512/hookbeam/pkg/mailer/memory"
	"github.com/bete7512/hookbeam/pkg/mailer/smtp"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     backend.Config
		want    any
		wantErr bool
	}{
		{name: "memory", cfg: backend.Config{Backend: "memory"}, want: &memory.Mailer{}},
		{name: "smtp", cfg: backend.Config{Backend: "smtp", SMTPAddr: "localhost:1025", From: "a@b"}, want: &smtp.Mailer{}},
		{name: "unknown", cfg: backend.Config{Backend: "pigeon"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := backend.New(context.Background(), tt.cfg)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.IsType(t, tt.want, m)
		})
	}
}
