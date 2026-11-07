// Package config loads configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/bete7512/hookbeam/pkg/db"
)

// Config holds the settings every service shares. Services embed it and add their own.
type Config struct {
	App  App  `envPrefix:"APP_"`
	HTTP HTTP `envPrefix:"HTTP_"`
	Log  Log  `envPrefix:"LOG_"`
	DB   db.Config
}

// App identifies the running service.
type App struct {
	Env     string `env:"ENV" envDefault:"dev"`
	Service string `env:"SERVICE" envDefault:"api"`
	Version string `env:"VERSION" envDefault:"dev"`
}

// HTTP configures the HTTP server.
type HTTP struct {
	Addr              string        `env:"ADDR" envDefault:":8080"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout       time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"30s"`
	MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"1048576"`
	TrustProxy        bool          `env:"TRUST_PROXY" envDefault:"false"` // read X-Forwarded-For (behind a load balancer)
}

// Log configures the logger.
type Log struct {
	Level  string `env:"LEVEL" envDefault:"debug"`
	Format string `env:"FORMAT" envDefault:"text"`
}

// Validate checks the environment name.
func (a App) Validate() error {
	if a.Env != "dev" && a.Env != "staging" && a.Env != "prod" {
		return errors.New("APP_ENV must be dev, staging or prod")
	}
	return nil
}

// Validate checks the level and the format.
func (l Log) Validate() error {
	var level slog.Level
	var errs []error
	if level.UnmarshalText([]byte(l.Level)) != nil {
		errs = append(errs, errors.New("LOG_LEVEL must be debug, info, warn or error"))
	}
	if l.Format != "text" && l.Format != "json" {
		errs = append(errs, errors.New("LOG_FORMAT must be text or json"))
	}
	return errors.Join(errs...)
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
	check := func(ok bool, msg string) {
		if !ok {
			errs = append(errs, errors.New(msg))
		}
	}

	if err := c.App.Validate(); err != nil {
		errs = append(errs, err)
	}
	check(c.HTTP.Addr != "", "HTTP_ADDR is required")
	check(c.HTTP.ReadHeaderTimeout > 0 && c.HTTP.ReadTimeout > 0 && c.HTTP.WriteTimeout > 0 &&
		c.HTTP.IdleTimeout > 0 && c.HTTP.ShutdownTimeout > 0, "HTTP timeouts must be positive")
	check(c.HTTP.MaxBodyBytes > 0, "HTTP_MAX_BODY_BYTES must be positive")
	if err := c.Log.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := c.DB.Validate(); err != nil {
		errs = append(errs, err)
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
		slog.String("http_addr", c.HTTP.Addr),
		slog.String("http_write_timeout", c.HTTP.WriteTimeout.String()),
		slog.String("log_level", c.Log.Level),
		slog.String("log_format", c.Log.Format),
		slog.String("db_url", dbURL),
		slog.Int("db_max_conns", int(c.DB.MaxConns)),
	)
}
