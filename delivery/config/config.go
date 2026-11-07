// Package config is the data plane's configuration. Its own settings carry the DELIVERY_ prefix so one local
// .env can hold both services; APP_* and LOG_* are shared.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/caarlos0/env/v11"

	base "github.com/bete7512/hookbeam/pkg/config"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/grpcx"
	"github.com/bete7512/hookbeam/pkg/secrets"
)

// Config is everything the delivery server and worker read.
type Config struct {
	App     base.App    `envPrefix:"APP_"`
	Log     base.Log    `envPrefix:"LOG_"`
	DB      db.Config   `envPrefix:"DELIVERY_"` // DELIVERY_DATABASE_URL, DELIVERY_DB_*
	GRPC    GRPC        `envPrefix:"DELIVERY_GRPC_"`
	TLS     grpcx.TLS   `envPrefix:"DELIVERY_TLS_"` // server certificate and the CA the control plane's client certificate chains to
	Events  base.Events `envPrefix:"DELIVERY_EVENTS_"`
	Outbox  base.Outbox `envPrefix:"DELIVERY_OUTBOX_"`
	Secrets Secrets     `envPrefix:"DELIVERY_SECRETS_"`
}

// GRPC configures the listener.
type GRPC struct {
	Addr            string        `env:"ADDR" envDefault:":9090"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`
}

// Secrets configures encryption at rest of endpoint signing secrets.
type Secrets struct {
	Key   string `env:"KEY"`                         // base64 of 32 random bytes
	KeyID string `env:"KEY_ID" envDefault:"2026-11"` // stored with every ciphertext so the key can rotate
}

// Load reads and validates the process environment.
func Load() (Config, error) {
	return parse(env.Options{})
}

// Parse reads and validates the given environment instead of the process one.
func Parse(environment map[string]string) (Config, error) {
	return parse(env.Options{Environment: environment})
}

func parse(opts env.Options) (Config, error) {
	var cfg Config
	if err := env.ParseWithOptions(&cfg, opts); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate returns every invalid setting at once.
func (c Config) Validate() error {
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	production := c.App.Env == "prod" || c.App.Env == "staging"
	add(c.App.Validate())
	add(c.Log.Validate())
	if err := c.DB.Validate(); err != nil {
		add(fmt.Errorf("DELIVERY_%w", err))
	}
	if c.GRPC.Addr == "" {
		add(errors.New("DELIVERY_GRPC_ADDR is required"))
	}
	if c.GRPC.ShutdownTimeout <= 0 {
		add(errors.New("DELIVERY_GRPC_SHUTDOWN_TIMEOUT must be positive"))
	}
	add(c.TLS.Validate("DELIVERY_TLS_", !production))
	add(c.Events.Validate("DELIVERY_EVENTS_", production))
	add(c.Outbox.Validate("DELIVERY_OUTBOX_"))
	if _, err := secrets.New(c.Secrets.Key); err != nil {
		add(errors.New("DELIVERY_SECRETS_KEY must be base64 of 32 random bytes (openssl rand -base64 32)"))
	}
	if c.Secrets.KeyID == "" {
		add(errors.New("DELIVERY_SECRETS_KEY_ID is required"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %w", errors.Join(errs...))
	}
	return nil
}

// LogValue logs the settings worth seeing at startup, with the database password masked.
func (c Config) LogValue() slog.Value {
	dbURL := "<invalid url>"
	if u, err := url.Parse(c.DB.URL); err == nil {
		dbURL = u.Redacted()
	}
	return slog.GroupValue(
		slog.String("grpc_addr", c.GRPC.Addr),
		slog.Bool("tls_insecure", c.TLS.Insecure),
		slog.String("log_level", c.Log.Level),
		slog.String("db_url", dbURL),
		slog.String("events_backend", c.Events.Backend),
	)
}
