package config_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/config"
)

const dbURL = "postgres://app:secret@localhost:5432/app"

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantErrs []string
		check    func(t *testing.T, cfg config.Config)
	}{
		{
			name: "defaults",
			env:  map[string]string{"DATABASE_URL": dbURL},
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "dev", cfg.App.Env)
				assert.Equal(t, ":8080", cfg.HTTP.Addr)
				assert.Equal(t, 15*time.Second, cfg.HTTP.ReadTimeout)
				assert.Equal(t, int32(10), cfg.DB.MaxConns)
			},
		},
		{
			name: "overrides",
			env:  map[string]string{"DATABASE_URL": dbURL, "APP_ENV": "prod", "HTTP_ADDR": ":9090", "DB_MAX_CONNS": "20", "LOG_FORMAT": "json"},
			check: func(t *testing.T, cfg config.Config) {
				assert.Equal(t, "prod", cfg.App.Env)
				assert.Equal(t, ":9090", cfg.HTTP.Addr)
				assert.Equal(t, int32(20), cfg.DB.MaxConns)
				assert.Equal(t, "json", cfg.Log.Format)
			},
		},
		{name: "missing database url", env: map[string]string{}, wantErrs: []string{"DATABASE_URL is required"}},
		{name: "unparsable value", env: map[string]string{"DATABASE_URL": dbURL, "HTTP_IDLE_TIMEOUT": "soon"}, wantErrs: []string{"soon"}},
		{
			name:     "reports every problem",
			env:      map[string]string{"DATABASE_URL": dbURL, "APP_ENV": "qa", "APP_TIMEZONE": "Mars/Olympus", "LOG_FORMAT": "xml", "HTTP_MAX_BODY_BYTES": "0"},
			wantErrs: []string{"APP_ENV", "APP_TIMEZONE", "LOG_FORMAT", "HTTP_MAX_BODY_BYTES"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Parse(tt.env)
			if len(tt.wantErrs) > 0 {
				require.Error(t, err)
				for _, want := range tt.wantErrs {
					assert.Contains(t, err.Error(), want)
				}
				return
			}
			require.NoError(t, err)
			tt.check(t, cfg)
		})
	}
}

func TestLoadReadsProcessEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("HTTP_ADDR", ":7070")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, ":7070", cfg.HTTP.Addr)
}

func TestLogValueMasksPassword(t *testing.T) {
	cfg, err := config.Parse(map[string]string{"DATABASE_URL": dbURL})
	require.NoError(t, err)

	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("config loaded", "config", cfg)

	assert.NotContains(t, buf.String(), "secret")
	assert.Contains(t, buf.String(), "localhost")
}
