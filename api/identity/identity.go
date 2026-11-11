// Package identity owns goauth: it builds the auth server over the service's pool, opens its storage for the
// processes that only read it, and adapts goauth's interfaces to Hookbeam's ports.
package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bete7512/hookbeam/pkg/depcheck"

	"github.com/bete7512/goauth/pkg/auth"
	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/bete7512/goauth/pkg/modules/notification"
	"github.com/bete7512/goauth/pkg/modules/oauth"
	"github.com/bete7512/goauth/pkg/modules/organization"
	"github.com/bete7512/goauth/pkg/modules/stateless"
	"github.com/bete7512/goauth/pkg/types"
	"github.com/bete7512/goauth/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/pkg/mailer"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

// Deps are what New needs; the limiter counts goauth's rate limits in the shared cache.
type Deps struct {
	Config  config.Config
	Pool    *pgxpool.Pool
	Mailer  mailer.Mailer
	Limiter ratelimit.Limiter
	Logger  *slog.Logger
}

// New builds goauth over the service's pool with every module registered, not yet initialized.
// Identity, sessions, Google login, account emails, rate limits and workspaces (goauth organizations) all live here.
// goauth's audit module is not registered: its routes require the admin module.
func New(d Deps) (*auth.Auth, error) {
	if err := depcheck.Required("identity", d); err != nil {
		return nil, err
	}
	pemKey, err := d.Config.Auth.PrivateKeyPEM()
	if err != nil {
		return nil, err
	}
	store, err := OpenStorage(d.Pool)
	if err != nil {
		return nil, err
	}

	var proxies []string
	if d.Config.HTTP.TrustProxy {
		proxies = []string{"0.0.0.0/0", "::/0"}
	}
	a, err := auth.New(&goauthcfg.Config{
		Storage:             store,
		BorrowStorage:       true,
		BasePath:            config.AuthBasePath,
		APIURL:              d.Config.Auth.APIURL,
		Logger:              slogLogger{log: d.Logger},
		TrustedProxies:      proxies,
		MaxRequestBodyBytes: d.Config.HTTP.MaxBodyBytes,
		Migration:           goauthcfg.MigrationConfig{Auto: false},
		Core:                &goauthcfg.CoreConfig{},
		RateLimit:           &goauthcfg.RateLimitModuleConfig{Store: rateLimitStore{limiter: d.Limiter}},
		FrontendConfig: &goauthcfg.FrontendConfig{
			URL:                     d.Config.Auth.LinkBaseURL,
			LoginPath:               "/login",
			SignupPath:              "/signup",
			ResetPasswordPath:       "/reset-password",
			VerifyEmailCallbackPath: "/verify-email",
		},
		Security: types.SecurityConfig{
			JWTIssuer:        d.Config.Auth.Issuer,
			JWTAudience:      d.Config.Auth.Audience,
			JwtSecretKey:     d.Config.Auth.SecretKey,
			EncryptionKey:    d.Config.Auth.EncryptionKey,
			JWTAlgorithm:     types.JWTAlgorithm(d.Config.Auth.JWTAlgorithm),
			JWTPrivateKeyPEM: pemKey,
			JWTKeyID:         d.Config.Auth.JWTKeyID,
			Session: types.SessionConfig{
				Name:                 config.SessionName,
				AccessTokenTTL:       d.Config.Auth.AccessTokenTTL,
				RefreshTokenTTL:      d.Config.Auth.RefreshTokenTTL,
				SessionTTL:           d.Config.Auth.RefreshTokenTTL,
				AllowInsecureCookies: d.Config.Auth.AllowInsecureCookies,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("identity: goauth: %w", err)
	}

	modules := []goauthcfg.Component{
		stateless.New(&goauthcfg.StatelessModuleConfig{RefreshTokenRotation: true}),
		notification.New(&notification.Config{
			EmailSender:        emailSender{mailer: d.Mailer},
			Branding:           &notification.Branding{AppName: "Hookbeam", PrimaryColor: "#f59e0b"},
			EnableWelcomeEmail: true,
		}),
		organization.New(&organization.Config{
			AutoCreateOrg:         true,
			DefaultOrgNamePattern: "{name}'s workspace",
			InvitationCallbackURL: d.Config.Auth.LinkBaseURL + "/invitations/accept",
		}),
	}
	if d.Config.OIDC.GoogleEnabled() {
		modules = append(modules, oauth.New(&goauthcfg.OAuthModuleConfig{
			Providers: map[string]*goauthcfg.OAuthProviderConfig{
				"google": {ClientID: d.Config.OIDC.GoogleClientID, ClientSecret: d.Config.OIDC.GoogleClientSecret},
			},
			DefaultRedirectURL: d.Config.Auth.LinkBaseURL + "/auth/callback",
			ErrorRedirectURL:   d.Config.Auth.LinkBaseURL + "/login",
			AllowSignup:        true,
		}))
	}
	for _, m := range modules {
		if err := a.Use(m); err != nil {
			return nil, fmt.Errorf("identity: goauth module: %w", err)
		}
	}
	return a, nil
}

// OpenStorage returns goauth's storage over the pool, for a process that reads identity data without serving it.
func OpenStorage(pool *pgxpool.Pool) (types.Storage, error) {
	if pool == nil {
		return nil, errors.New("identity: Pool is required")
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDBFromPool(pool)}), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return nil, fmt.Errorf("identity: goauth storage: %w", err)
	}
	return storage.NewGormStorageFromDB(gormDB), nil
}

// Migrate applies goauth's own migrations; `api -m migrate up` calls it after goose.
func Migrate(ctx context.Context, a *auth.Auth) error {
	return a.ApplyMigrations(ctx)
}
