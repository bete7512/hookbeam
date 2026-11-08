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
	Claims  Claims      `envPrefix:"DELIVERY_CLAIM_"`
	Sender  Sender      `envPrefix:"DELIVERY_SENDER_"`
	Payload Payload     `envPrefix:"DELIVERY_PAYLOAD_"`
	Audit   Audit       `envPrefix:"DELIVERY_AUDIT_"`
}

// Claims configures the worker's claim loop.
type Claims struct {
	Interval  time.Duration `env:"INTERVAL" envDefault:"5s"` // poll for due work between wake-ups
	BatchSize int           `env:"BATCH_SIZE" envDefault:"20"`
	LeaseTTL  time.Duration `env:"LEASE_TTL" envDefault:"60s"`
	// EndpointConcurrency caps a destination's in-flight attempts across every worker replica.
	EndpointConcurrency int `env:"ENDPOINT_CONCURRENCY" envDefault:"2"`
	// Concurrency is how many attempts one worker runs at the same time, so a stalling destination
	// does not hold up the rest of its batch.
	Concurrency int `env:"CONCURRENCY" envDefault:"10"`
	// SweepInterval is how often the worker recovers deliveries whose lease expired without an outcome.
	SweepInterval time.Duration `env:"SWEEP_INTERVAL" envDefault:"30s"`
}

// Sender bounds outbound webhook requests.
type Sender struct {
	Timeout          time.Duration `env:"TIMEOUT" envDefault:"10s"`
	AllowedPorts     []int         `env:"ALLOWED_PORTS" envDefault:"443,8443"`
	AllowPrivate     bool          `env:"ALLOW_PRIVATE" envDefault:"false"` // http and private addresses: local development only
	MaxResponseBytes int64         `env:"MAX_RESPONSE_BYTES" envDefault:"65536"`
	MaxPreviewBytes  int           `env:"MAX_PREVIEW_BYTES" envDefault:"1024"`
}

// Payload bounds what is accepted and how long it is kept.
type Payload struct {
	MaxBytes          int           `env:"MAX_BYTES" envDefault:"262144"`
	Retention         time.Duration `env:"RETENTION" envDefault:"168h"`
	IdempotencyWindow time.Duration `env:"IDEMPOTENCY_WINDOW" envDefault:"168h"`
	// ExpiryInterval is how often the worker drops expired payloads and idempotency keys, ExpiryBatchSize rows at a time.
	ExpiryInterval  time.Duration `env:"EXPIRY_INTERVAL" envDefault:"10m"`
	ExpiryBatchSize int           `env:"EXPIRY_BATCH_SIZE" envDefault:"500"`
}

// Audit configures the audit trail.
type Audit struct {
	Retention time.Duration `env:"RETENTION" envDefault:"2160h"` // 90 days
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
	if c.Claims.Interval <= 0 || c.Claims.BatchSize <= 0 || c.Claims.LeaseTTL <= 0 || c.Claims.SweepInterval <= 0 || c.Claims.EndpointConcurrency <= 0 || c.Claims.Concurrency <= 0 {
		add(errors.New("DELIVERY_CLAIM_INTERVAL, DELIVERY_CLAIM_BATCH_SIZE, DELIVERY_CLAIM_LEASE_TTL, DELIVERY_CLAIM_SWEEP_INTERVAL, DELIVERY_CLAIM_ENDPOINT_CONCURRENCY and DELIVERY_CLAIM_CONCURRENCY must be positive"))
	}
	if c.Sender.Timeout <= 0 || c.Sender.MaxResponseBytes <= 0 || c.Sender.MaxPreviewBytes <= 0 {
		add(errors.New("DELIVERY_SENDER_TIMEOUT, DELIVERY_SENDER_MAX_RESPONSE_BYTES and DELIVERY_SENDER_MAX_PREVIEW_BYTES must be positive"))
	}
	if c.Sender.AllowPrivate && production {
		add(errors.New("DELIVERY_SENDER_ALLOW_PRIVATE is for local development only"))
	}
	if c.Audit.Retention <= 0 {
		add(errors.New("DELIVERY_AUDIT_RETENTION must be positive"))
	}
	if c.Payload.MaxBytes <= 0 || c.Payload.Retention <= 0 || c.Payload.IdempotencyWindow <= 0 || c.Payload.ExpiryInterval <= 0 || c.Payload.ExpiryBatchSize <= 0 {
		add(errors.New("DELIVERY_PAYLOAD_MAX_BYTES, DELIVERY_PAYLOAD_RETENTION, DELIVERY_PAYLOAD_IDEMPOTENCY_WINDOW, DELIVERY_PAYLOAD_EXPIRY_INTERVAL and DELIVERY_PAYLOAD_EXPIRY_BATCH_SIZE must be positive"))
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
