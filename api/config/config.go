// Package config is the control plane's configuration: the shared settings plus its own.
package config

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	base "github.com/bete7512/hookbeam/pkg/config"
	"github.com/bete7512/hookbeam/pkg/grpcx"
)

const (
	// AuthBasePath is where goauth's routes are mounted.
	AuthBasePath = "/v1/auth"
	// SessionName names goauth's credential cookies: goauth_access_<name>, goauth_refresh_<name>.
	SessionName = "hookbeam"
	// AccessCookie carries the access token for browser clients.
	AccessCookie = "goauth_access_" + SessionName
)

// Config is the shared configuration plus the control plane's settings.
type Config struct {
	base.Config
	Auth   Auth   `envPrefix:"AUTH_"`
	Events Events `envPrefix:"EVENTS_"`
	Outbox Outbox `envPrefix:"OUTBOX_"`
	Mailer Mailer `envPrefix:"MAILER_"`
	Cache  Cache  `envPrefix:"CACHE_"`
	OIDC   OIDC   `envPrefix:"OIDC_"`
	// Delivery is the client side of the gRPC connection to the data plane.
	Delivery Delivery `envPrefix:"DELIVERY_CLIENT_"`
}

// Delivery configures the connection to the delivery service.
type Delivery struct {
	Addr       string        `env:"ADDR" envDefault:"localhost:9090"`
	ServerName string        `env:"SERVER_NAME"` // name in the server certificate; empty uses the host of Addr
	Timeout    time.Duration `env:"TIMEOUT" envDefault:"5s"`
	TLS        grpcx.TLS     `envPrefix:"TLS_"` // client certificate and the CA the server certificate chains to
}

// Cache sizes the in-process cache shared by the service (rate-limit counters today).
type Cache struct {
	MaxItems int64 `env:"MAX_ITEMS" envDefault:"100000"`
}

// OIDC configures Google login; the provider is registered only when ClientID is set.
type OIDC struct {
	GoogleClientID     string `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET"`
}

// GoogleEnabled reports whether Google login is configured.
func (o OIDC) GoogleEnabled() bool { return o.GoogleClientID != "" }

// Events is the shared transport configuration under the EVENTS_ prefix.
type Events = base.Events

// Outbox is the shared relay configuration under the OUTBOX_ prefix.
type Outbox = base.Outbox

// Mailer configures outbound email.
type Mailer struct {
	Backend  string `env:"BACKEND" envDefault:"memory"` // smtp | ses | memory
	SMTPAddr string `env:"SMTP_ADDR" envDefault:"localhost:1025"`
	From     string `env:"FROM" envDefault:"no-reply@hookbeam.local"`
}

// Auth configures goauth: token signing, cookie encoding, encryption at rest and the console links.
type Auth struct {
	JWTAlgorithm         string        `env:"JWT_ALGORITHM" envDefault:"ES256"` // ES256 | RS256 | HS256
	JWTPrivateKey        string        `env:"JWT_PRIVATE_KEY"`                  // base64 of a PKCS#8 PEM; alternative to the file
	JWTPrivateKeyFile    string        `env:"JWT_PRIVATE_KEY_FILE"`
	JWTKeyID             string        `env:"JWT_KEY_ID" envDefault:"2026-11"`
	SecretKey            string        `env:"SECRET_KEY"`     // at least 32 characters: cookie encoding and HS256
	EncryptionKey        string        `env:"ENCRYPTION_KEY"` // at least 32 characters: secrets at rest
	Issuer               string        `env:"ISSUER" envDefault:"hookbeam"`
	Audience             string        `env:"AUDIENCE" envDefault:"hookbeam"`
	AccessTokenTTL       time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL      time.Duration `env:"REFRESH_TOKEN_TTL" envDefault:"720h"`
	LinkBaseURL          string        `env:"LINK_BASE_URL" envDefault:"http://localhost:3000"` // console origin the links in emails point to
	APIURL               string        `env:"API_URL" envDefault:"http://localhost:8080"`       // public origin of this API; OAuth callbacks land here
	AllowInsecureCookies bool          `env:"ALLOW_INSECURE_COOKIES" envDefault:"false"`        // plain-HTTP development only
}

// PrivateKeyPEM returns the signing key PEM from the inline value or the file.
func (a Auth) PrivateKeyPEM() ([]byte, error) {
	switch {
	case a.JWTPrivateKey != "":
		return base64.StdEncoding.DecodeString(a.JWTPrivateKey)
	case a.JWTPrivateKeyFile != "":
		return os.ReadFile(a.JWTPrivateKeyFile)
	default:
		return nil, nil
	}
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
	production := c.App.Env == "prod" || c.App.Env == "staging"
	if err := c.Config.Validate(); err != nil {
		errs = append(errs, err)
	}
	if len(c.Auth.SecretKey) < 32 {
		errs = append(errs, errors.New("AUTH_SECRET_KEY must be at least 32 characters (openssl rand -base64 32)"))
	}
	if len(c.Auth.EncryptionKey) < 32 {
		errs = append(errs, errors.New("AUTH_ENCRYPTION_KEY must be at least 32 characters (openssl rand -base64 32)"))
	}
	if c.Auth.SecretKey != "" && c.Auth.SecretKey == c.Auth.EncryptionKey {
		errs = append(errs, errors.New("AUTH_ENCRYPTION_KEY must differ from AUTH_SECRET_KEY"))
	}
	if err := c.Auth.validateSigningKey(); err != nil {
		errs = append(errs, err)
	}
	if c.Auth.JWTKeyID == "" {
		errs = append(errs, errors.New("AUTH_JWT_KEY_ID is required"))
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("AUTH_ACCESS_TOKEN_TTL and AUTH_REFRESH_TOKEN_TTL must be positive"))
	}
	if !validOrigin(c.Auth.LinkBaseURL) || !validOrigin(c.Auth.APIURL) {
		errs = append(errs, errors.New("AUTH_LINK_BASE_URL and AUTH_API_URL must be http(s) origins"))
	}
	if c.Delivery.Addr == "" {
		errs = append(errs, errors.New("DELIVERY_CLIENT_ADDR is required"))
	}
	if c.Delivery.Timeout <= 0 {
		errs = append(errs, errors.New("DELIVERY_CLIENT_TIMEOUT must be positive"))
	}
	if err := c.Delivery.TLS.Validate("DELIVERY_CLIENT_TLS_", !production); err != nil {
		errs = append(errs, err)
	}
	if c.Cache.MaxItems <= 0 {
		errs = append(errs, errors.New("CACHE_MAX_ITEMS must be positive"))
	}
	if c.OIDC.GoogleEnabled() && c.OIDC.GoogleClientSecret == "" {
		errs = append(errs, errors.New("OIDC_GOOGLE_CLIENT_SECRET is required with OIDC_GOOGLE_CLIENT_ID"))
	}
	if err := c.Events.Validate("EVENTS_", production); err != nil {
		errs = append(errs, err)
	}
	if err := c.Outbox.Validate("OUTBOX_"); err != nil {
		errs = append(errs, err)
	}
	switch c.Mailer.Backend {
	case "memory", "smtp", "ses":
	default:
		errs = append(errs, errors.New("MAILER_BACKEND must be smtp, ses or memory"))
	}
	if c.Mailer.From == "" {
		errs = append(errs, errors.New("MAILER_FROM is required"))
	}
	if production {
		if c.Mailer.Backend != "ses" {
			errs = append(errs, errors.New("staging/prod require MAILER_BACKEND=ses; the current SMTP adapter is for local development"))
		}
		if c.Auth.AllowInsecureCookies {
			errs = append(errs, errors.New("AUTH_ALLOW_INSECURE_COOKIES is for local development only"))
		}
		if c.Auth.JWTAlgorithm == "HS256" {
			errs = append(errs, errors.New("staging/prod require an asymmetric AUTH_JWT_ALGORITHM so other services can verify tokens through JWKS"))
		}
		if !publicHTTPS(c.Auth.LinkBaseURL) || !publicHTTPS(c.Auth.APIURL) {
			errs = append(errs, errors.New("AUTH_LINK_BASE_URL and AUTH_API_URL must use public HTTPS origins in staging/prod"))
		}
	}
	return errors.Join(errs...)
}

func (a Auth) validateSigningKey() error {
	switch a.JWTAlgorithm {
	case "HS256":
		return nil
	case "ES256", "RS256":
	default:
		return errors.New("AUTH_JWT_ALGORITHM must be ES256, RS256 or HS256")
	}
	data, err := a.PrivateKeyPEM()
	if err != nil {
		return fmt.Errorf("AUTH_JWT_PRIVATE_KEY: %w", err)
	}
	if len(data) == 0 {
		return errors.New("AUTH_JWT_PRIVATE_KEY (base64 PEM) or AUTH_JWT_PRIVATE_KEY_FILE is required with " + a.JWTAlgorithm)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return errors.New("AUTH_JWT_PRIVATE_KEY is not PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("AUTH_JWT_PRIVATE_KEY must be a PKCS#8 private key: %w", err)
	}
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		if a.JWTAlgorithm != "ES256" {
			return errors.New("RS256 needs an RSA key, got an EC key")
		}
		if k.Curve.Params().Name != "P-256" {
			return errors.New("ES256 needs a P-256 EC key")
		}
	case *rsa.PrivateKey:
		if a.JWTAlgorithm != "RS256" {
			return errors.New("ES256 needs an EC key, got an RSA key")
		}
		if k.N.BitLen() < 2048 {
			return errors.New("RS256 needs an RSA key of at least 2048 bits")
		}
	default:
		return errors.New("AUTH_JWT_PRIVATE_KEY must be an EC P-256 or RSA key")
	}
	return nil
}

func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
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
