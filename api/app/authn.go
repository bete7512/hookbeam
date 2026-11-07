package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bete7512/goauth/pkg/auth"
	goauthcfg "github.com/bete7512/goauth/pkg/config"
	"github.com/bete7512/goauth/pkg/modules/notification"
	"github.com/bete7512/goauth/pkg/modules/oauth"
	"github.com/bete7512/goauth/pkg/modules/stateless"
	"github.com/bete7512/goauth/pkg/types"
	"github.com/bete7512/goauth/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/pkg/logger"
	"github.com/bete7512/hookbeam/pkg/mailer"
)

// AuthDeps are what NewAuth needs; the limiter counts in the shared cache.
type AuthDeps struct {
	Config  config.Config
	Pool    *pgxpool.Pool
	Mailer  mailer.Mailer
	Limiter types.RateLimitStore
	Logger  *slog.Logger
}

// NewAuth builds goauth over the service's pool with every module registered, not yet initialized.
// Identity, sessions, Google login, account emails and rate limits all live here.
// goauth's audit module is not registered: its routes require the admin module.
func NewAuth(d AuthDeps) (*auth.Auth, error) {
	if d.Pool == nil || d.Mailer == nil || d.Limiter == nil || d.Logger == nil {
		return nil, errors.New("app: Pool, Mailer, Limiter and Logger are required")
	}
	pemKey, err := d.Config.Auth.PrivateKeyPEM()
	if err != nil {
		return nil, err
	}
	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDBFromPool(d.Pool)}), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return nil, fmt.Errorf("app: goauth storage: %w", err)
	}

	var proxies []string
	if d.Config.HTTP.TrustProxy {
		proxies = []string{"0.0.0.0/0", "::/0"}
	}
	a, err := auth.New(&goauthcfg.Config{
		Storage:             storage.NewGormStorageFromDB(gormDB),
		BorrowStorage:       true,
		BasePath:            config.AuthBasePath,
		APIURL:              d.Config.Auth.APIURL,
		Logger:              logger.NewGoAuth(d.Logger),
		TrustedProxies:      proxies,
		MaxRequestBodyBytes: d.Config.HTTP.MaxBodyBytes,
		Migration:           goauthcfg.MigrationConfig{Auto: false},
		Core:                &goauthcfg.CoreConfig{},
		RateLimit:           &goauthcfg.RateLimitModuleConfig{Store: d.Limiter},
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
		return nil, fmt.Errorf("app: goauth: %w", err)
	}

	modules := []goauthcfg.Component{
		stateless.New(&goauthcfg.StatelessModuleConfig{RefreshTokenRotation: true}),
		notification.New(&notification.Config{
			EmailSender:        mailer.NewGoAuthSender(d.Mailer),
			Branding:           &notification.Branding{AppName: "Hookbeam", PrimaryColor: "#f59e0b"},
			EnableWelcomeEmail: true,
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
			return nil, fmt.Errorf("app: goauth module: %w", err)
		}
	}
	return a, nil
}

// MigrateAuth applies goauth's own migrations; called by `api -m migrate up` after goose.
func MigrateAuth(ctx context.Context, a *auth.Auth) error {
	return a.ApplyMigrations(ctx)
}
