// Package config is the api service's configuration: the shared settings plus its own.
package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	base "github.com/bete7512/scaffold/pkg/config"
)

// Config is the shared configuration plus the api service's settings.
type Config struct {
	base.Config
	Auth      Auth      `envPrefix:"AUTH_"`
	Events    Events    `envPrefix:"EVENTS_"`
	Outbox    Outbox    `envPrefix:"OUTBOX_"`
	Mailer    Mailer    `envPrefix:"MAILER_"`
	RateLimit RateLimit `envPrefix:"RATELIMIT_"`
	Cache     Cache     `envPrefix:"CACHE_"`
	OIDC      OIDC      `envPrefix:"OIDC_"`
}

// Cache sizes the in-process cache shared by the app (rate-limit counters today).
type Cache struct {
	MaxItems int64 `env:"MAX_ITEMS" envDefault:"100000"`
}

// RateLimit configures the auth-route limits: per client IP and per account on login.
type RateLimit struct {
	RegisterLimit  int           `env:"REGISTER_LIMIT" envDefault:"5"`
	RegisterWindow time.Duration `env:"REGISTER_WINDOW" envDefault:"1m"`
	RecoveryLimit  int           `env:"RECOVERY_LIMIT" envDefault:"5"`
	RecoveryWindow time.Duration `env:"RECOVERY_WINDOW" envDefault:"1m"`
	SessionLimit   int           `env:"SESSION_LIMIT" envDefault:"60"`
	SessionWindow  time.Duration `env:"SESSION_WINDOW" envDefault:"1m"`
	AuthLimit      int           `env:"AUTH_LIMIT" envDefault:"5"`
	AuthWindow     time.Duration `env:"AUTH_WINDOW" envDefault:"1m"`
	LoginLimit     int           `env:"LOGIN_LIMIT" envDefault:"5"`
	LoginWindow    time.Duration `env:"LOGIN_WINDOW" envDefault:"15m"`
}

// OIDC configures Google login; the routes answer 503 until ClientID is set.
type OIDC struct {
	DiscoveryTimeout   time.Duration `env:"DISCOVERY_TIMEOUT" envDefault:"10s"`
	GoogleClientID     string        `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string        `env:"GOOGLE_CLIENT_SECRET"`
	GoogleRedirectURL  string        `env:"GOOGLE_REDIRECT_URL" envDefault:"http://localhost:8080/v1/auth/google/callback"`
}

// GoogleEnabled reports whether Google login is configured.
func (o OIDC) GoogleEnabled() bool { return o.GoogleClientID != "" }

// Events configures the event transport. AWS credentials come from the standard AWS_* variables.
type Events struct {
	HandlerTimeout time.Duration `env:"HANDLER_TIMEOUT" envDefault:"2m"`
	Backend        string        `env:"BACKEND" envDefault:"memory"` // aws | memory
	Region         string        `env:"AWS_REGION" envDefault:"us-east-1"`
	Endpoint       string        `env:"AWS_ENDPOINT_URL"` // LocalStack; empty in AWS
	TopicARN       string        `env:"TOPIC_ARN"`
	QueueURL       string        `env:"QUEUE_URL"`
}

// Outbox configures the relay and cleanup that run in the worker.
type Outbox struct {
	PublishTimeout time.Duration `env:"PUBLISH_TIMEOUT" envDefault:"30s"`
	PollInterval   time.Duration `env:"POLL_INTERVAL" envDefault:"500ms"`
	BatchSize      int           `env:"BATCH_SIZE" envDefault:"100"`
	Retention      time.Duration `env:"RETENTION" envDefault:"168h"`
}

// Mailer configures outbound email.
type Mailer struct {
	Backend  string `env:"BACKEND" envDefault:"memory"` // smtp | ses | memory
	SMTPAddr string `env:"SMTP_ADDR" envDefault:"localhost:1025"`
	From     string `env:"FROM" envDefault:"no-reply@scaffold.local"`
}

// Auth configures password hashing.
type Auth struct {
	ActionTokenKey    string        `env:"ACTION_TOKEN_KEY"`
	RecipientCooldown time.Duration `env:"RECIPIENT_COOLDOWN" envDefault:"1m"`
	Argon2MemoryKiB   uint32        `env:"ARGON2_MEMORY_KIB" envDefault:"19456"`
	Argon2Iterations  uint32        `env:"ARGON2_ITERATIONS" envDefault:"2"`
	Argon2Parallelism uint8         `env:"ARGON2_PARALLELISM" envDefault:"1"`

	JWTSigningKey   string        `env:"JWT_SIGNING_KEY"`
	Issuer          string        `env:"ISSUER"`
	AccessTokenTTL  time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"720h"`
	ActionTokenTTL  time.Duration `env:"ACTION_TOKEN_TTL" envDefault:"30m"` // email verification and password reset links
	LinkBaseURL     string        `env:"LINK_BASE_URL" envDefault:"http://localhost:3000"`
}

// SigningKeySeed returns the decoded ed25519 seed for signing access tokens.
func (a Auth) SigningKeySeed() []byte {
	seed, _ := base64.StdEncoding.DecodeString(a.JWTSigningKey)
	return seed
}

// ActionKey returns the independent secret used to derive retry-stable email tokens.
func (a Auth) ActionKey() []byte {
	key, _ := base64.StdEncoding.DecodeString(a.ActionTokenKey)
	return key
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
	if err := c.Config.Validate(); err != nil {
		errs = append(errs, err)
	}
	if c.Auth.Argon2MemoryKiB < 8*1024 {
		errs = append(errs, errors.New("AUTH_ARGON2_MEMORY_KIB must be at least 8192"))
	}
	if c.Auth.Argon2Iterations == 0 {
		errs = append(errs, errors.New("AUTH_ARGON2_ITERATIONS must be positive"))
	}
	if c.Auth.Argon2Parallelism == 0 {
		errs = append(errs, errors.New("AUTH_ARGON2_PARALLELISM must be positive"))
	}
	if len(c.Auth.ActionKey()) != 32 || bytes.Equal(c.Auth.ActionKey(), c.Auth.SigningKeySeed()) {
		errs = append(errs, errors.New("AUTH_ACTION_TOKEN_KEY must be base64 of 32 random bytes and different from AUTH_JWT_SIGNING_KEY"))
	}
	if c.Auth.RecipientCooldown <= 0 {
		errs = append(errs, errors.New("AUTH_RECIPIENT_COOLDOWN must be positive"))
	}
	if c.OIDC.DiscoveryTimeout <= 0 || c.Events.HandlerTimeout <= 0 || c.Outbox.PublishTimeout <= 0 {
		errs = append(errs, errors.New("OIDC_DISCOVERY_TIMEOUT, EVENTS_HANDLER_TIMEOUT and OUTBOX_PUBLISH_TIMEOUT must be positive"))
	}
	if c.Auth.ActionTokenTTL <= 0 || c.Auth.LinkBaseURL == "" {
		errs = append(errs, errors.New("AUTH_ACTION_TOKEN_TTL must be positive and AUTH_LINK_BASE_URL set"))
	}
	if c.RateLimit.AuthLimit <= 0 || c.RateLimit.AuthWindow <= 0 || c.RateLimit.LoginLimit <= 0 || c.RateLimit.LoginWindow <= 0 {
		errs = append(errs, errors.New("RATELIMIT_* limits and windows must be positive"))
	}
	if c.RateLimit.RegisterLimit <= 0 || c.RateLimit.RegisterWindow <= 0 || c.RateLimit.RecoveryLimit <= 0 || c.RateLimit.RecoveryWindow <= 0 || c.RateLimit.SessionLimit <= 0 || c.RateLimit.SessionWindow <= 0 {
		errs = append(errs, errors.New("RATELIMIT_* limits and windows must be positive"))
	}
	if c.Cache.MaxItems <= 0 {
		errs = append(errs, errors.New("CACHE_MAX_ITEMS must be positive"))
	}
	if c.OIDC.GoogleEnabled() && (c.OIDC.GoogleClientSecret == "" || c.OIDC.GoogleRedirectURL == "") {
		errs = append(errs, errors.New("OIDC_GOOGLE_CLIENT_SECRET and OIDC_GOOGLE_REDIRECT_URL are required with OIDC_GOOGLE_CLIENT_ID"))
	}
	if len(c.Auth.SigningKeySeed()) != 32 {
		errs = append(errs, errors.New("AUTH_JWT_SIGNING_KEY must be base64 of 32 random bytes (openssl rand -base64 32)"))
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("AUTH_ACCESS_TOKEN_TTL and AUTH_REFRESH_TOKEN_TTL must be positive"))
	}
	switch c.Events.Backend {
	case "memory":
	case "aws":
		if c.Events.TopicARN == "" || c.Events.QueueURL == "" {
			errs = append(errs, errors.New("EVENTS_TOPIC_ARN and EVENTS_QUEUE_URL are required when EVENTS_BACKEND=aws"))
		}
	default:
		errs = append(errs, errors.New("EVENTS_BACKEND must be aws or memory"))
	}
	if c.Outbox.PollInterval <= 0 || c.Outbox.BatchSize <= 0 || c.Outbox.Retention <= 0 {
		errs = append(errs, errors.New("OUTBOX_POLL_INTERVAL, OUTBOX_BATCH_SIZE and OUTBOX_RETENTION must be positive"))
	}
	switch c.Mailer.Backend {
	case "memory", "smtp", "ses":
	default:
		errs = append(errs, errors.New("MAILER_BACKEND must be smtp, ses or memory"))
	}
	if c.Mailer.From == "" {
		errs = append(errs, errors.New("MAILER_FROM is required"))
	}
	if c.App.Env == "prod" || c.App.Env == "staging" {
		if c.Events.Backend != "aws" || c.Events.Endpoint != "" {
			errs = append(errs, errors.New("staging/prod require AWS events without a local endpoint override"))
		}
		if c.Mailer.Backend != "ses" {
			errs = append(errs, errors.New("staging/prod require MAILER_BACKEND=ses; the current SMTP adapter is for local development"))
		}
		if c.Auth.Issuer == "" {
			errs = append(errs, errors.New("AUTH_ISSUER is required in staging/prod"))
		}
		if !publicHTTPS(c.Auth.LinkBaseURL) {
			errs = append(errs, errors.New("AUTH_LINK_BASE_URL must use a public HTTPS origin in staging/prod"))
		}
		if c.OIDC.GoogleEnabled() && !publicHTTPS(c.OIDC.GoogleRedirectURL) {
			errs = append(errs, errors.New("OIDC_GOOGLE_REDIRECT_URL must use public HTTPS in staging/prod"))
		}
	}
	return errors.Join(errs...)
}

func publicHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return false
	}
	return true
}
