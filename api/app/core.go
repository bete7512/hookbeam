// Package app wires the dependencies shared by every binary.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bete7512/goauth/pkg/auth"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"

	"github.com/bete7512/hookbeam/api/config"
	"github.com/bete7512/hookbeam/api/repos"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/cache"
	"github.com/bete7512/hookbeam/pkg/db"
	"github.com/bete7512/hookbeam/pkg/events"
	"github.com/bete7512/hookbeam/pkg/events/transport"
	"github.com/bete7512/hookbeam/pkg/mailer"
	mailermemory "github.com/bete7512/hookbeam/pkg/mailer/memory"
	"github.com/bete7512/hookbeam/pkg/mailer/ses"
	"github.com/bete7512/hookbeam/pkg/mailer/smtp"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
	ratelimitmemory "github.com/bete7512/hookbeam/pkg/ratelimit/memory"
	"github.com/bete7512/hookbeam/pkg/telemetry"
)

// Deps are what NewCore needs.
type Deps struct {
	Config config.Config
	Logger *slog.Logger
}

// Core holds the resources and services shared by the api and the worker.
type Core struct {
	Metrics     *telemetry.Metrics
	Pool        *pgxpool.Pool
	Auth        *auth.Auth
	APIKeys     services.APIKeyService
	Events      services.EventService
	Cache       *cache.Cache
	RateLimiter ratelimit.Limiter
	Publisher   events.Publisher
	Subscriber  events.Subscriber
	Mailer      mailer.Mailer
	// Delivery is the data plane behind the gRPC contract; DeliveryHealth answers /readyz for it.
	Delivery       services.DeliveryService
	DeliveryHealth Pinger
	// EndpointHealth is the projection the worker builds from delivery state events.
	EndpointHealth services.EndpointHealthService
	// Authz answers the role matrix; Members manages developer/viewer roles; OrgPolicy guards goauth's member routes.
	Authz     services.Authorizer
	Members   services.MemberService
	OrgPolicy services.OrgPolicy
	Audit     services.AuditService
	// Notifications turns health changes into notices and emails and serves a user's inbox.
	Notifications services.NotificationService

	deliveryConn *grpc.ClientConn
}

// Pinger reports whether a dependency answers; *pgxpool.Pool satisfies it too.
type Pinger interface {
	Ping(ctx context.Context) error
}

// NewCore opens the database pool, builds repos and services, and initializes goauth.
func NewCore(ctx context.Context, d Deps) (_ *Core, err error) {
	pool, err := db.NewPoolWithRetries(ctx, d.Logger, d.Config.DB)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			pool.Close()
		}
	}()

	outboxRepo, err := repos.NewOutboxRepo(pool)
	if err != nil {
		return nil, err
	}
	processedRepo, err := repos.NewProcessedEventRepo(pool)
	if err != nil {
		return nil, err
	}
	apiKeyRepo, err := repos.NewAPIKeyRepo(pool)
	if err != nil {
		return nil, err
	}
	healthRepo, err := repos.NewEndpointHealthRepo(pool)
	if err != nil {
		return nil, err
	}
	memberRoleRepo, err := repos.NewMemberRoleRepo(pool)
	if err != nil {
		return nil, err
	}
	auditRepo, err := repos.NewAuditRepo(pool)
	if err != nil {
		return nil, err
	}
	notificationRepo, err := repos.NewNotificationRepo(pool)
	if err != nil {
		return nil, err
	}
	preferenceRepo, err := repos.NewNotificationPreferenceRepo(pool)
	if err != nil {
		return nil, err
	}
	tx := db.NewTransactor(pool)
	metrics, err := telemetry.NewMetrics()
	if err != nil {
		return nil, err
	}
	appCache, err := cache.New(cache.Config{MaxItems: d.Config.Cache.MaxItems})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			appCache.Close()
		}
	}()
	limiter := ratelimitmemory.New(appCache)

	publisher, subscriber, err := transport.New(ctx, d.Config.Events, d.Logger)
	if err != nil {
		return nil, err
	}
	mail, err := newMailer(ctx, d.Config.Mailer, d.Config.Events.Region)
	if err != nil {
		return nil, err
	}

	deliveryConn, err := newDeliveryConn(d.Config.Delivery)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = deliveryConn.Close()
		}
	}()

	authn, err := NewAuth(AuthDeps{Config: d.Config, Pool: pool, Mailer: mail, Limiter: limiter, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	if err := authn.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("app: goauth initialize: %w", err)
	}
	defer func() {
		if err != nil {
			_ = authn.Close()
		}
	}()

	members := memberships{store: authn.Config().Storage.Organization()}
	authz, err := services.NewAuthorizer(services.AuthorizerDeps{Memberships: members, Roles: memberRoleRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	memberService, err := services.NewMemberService(services.MemberServiceDeps{Tx: tx, Memberships: members, Roles: memberRoleRepo, Audit: auditRepo, Authz: authz, Users: users{store: authn.Config().Storage.Core()}, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	orgPolicy, err := services.NewOrgPolicy(services.OrgPolicyDeps{Memberships: members, Authz: authz, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	apiKeys, err := services.NewAPIKeyService(services.APIKeyServiceDeps{Tx: tx, Keys: apiKeyRepo, Audit: auditRepo, Authz: authz, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	endpointHealth, err := services.NewEndpointHealthService(services.EndpointHealthServiceDeps{Health: healthRepo, Logger: d.Logger})
	if err != nil {
		return nil, err
	}

	delivery, err := services.NewDeliveryService(services.DeliveryServiceDeps{
		Authz:   authz,
		Client:  deliveryv1.NewDeliveryServiceClient(deliveryConn),
		Timeout: d.Config.Delivery.Timeout,
		Logger:  d.Logger,
	})
	if err != nil {
		return nil, err
	}

	eventService, err := services.NewEventService(services.EventServiceDeps{
		Metrics:        metrics,
		Tx:             tx,
		Outbox:         outboxRepo,
		Processed:      processedRepo,
		Publisher:      publisher,
		BatchSize:      d.Config.Outbox.BatchSize,
		Retention:      d.Config.Outbox.Retention,
		PublishTimeout: d.Config.Outbox.PublishTimeout,
		Logger:         d.Logger,
	})
	if err != nil {
		return nil, err
	}

	notifications, err := services.NewNotificationService(services.NotificationServiceDeps{
		Tx: tx, Notifications: notificationRepo, Preferences: preferenceRepo, Outbox: outboxRepo, Memberships: members,
		Users: users{store: authn.Config().Storage.Core()}, Delivery: delivery, Mailer: mail, LinkBaseURL: d.Config.Auth.LinkBaseURL, Logger: d.Logger,
	})
	if err != nil {
		return nil, err
	}
	auditService, err := services.NewAuditService(services.AuditServiceDeps{Audit: auditRepo, Delivery: delivery, Authz: authz, Retention: d.Config.Audit.Retention, BatchSize: d.Config.Outbox.BatchSize, Logger: d.Logger})
	if err != nil {
		return nil, err
	}
	return &Core{Metrics: metrics,
		Pool:           pool,
		Auth:           authn,
		APIKeys:        apiKeys,
		Events:         eventService,
		EndpointHealth: endpointHealth,
		Authz:          authz,
		Members:        memberService,
		OrgPolicy:      orgPolicy,
		Audit:          auditService,
		Notifications:  notifications,
		Cache:          appCache,
		RateLimiter:    limiter,
		Publisher:      publisher,
		Subscriber:     subscriber,
		Mailer:         mail,

		Delivery:       delivery,
		DeliveryHealth: deliveryPinger{conn: deliveryConn},
		deliveryConn:   deliveryConn,
	}, nil
}

func newMailer(ctx context.Context, cfg config.Mailer, region string) (mailer.Mailer, error) {
	switch cfg.Backend {
	case "memory":
		return mailermemory.New(), nil
	case "smtp":
		return smtp.New(smtp.Config{Addr: cfg.SMTPAddr, From: cfg.From}), nil
	case "ses":
		client, err := ses.NewClient(ctx, region)
		if err != nil {
			return nil, err
		}
		return ses.New(client, cfg.From), nil
	default:
		return nil, fmt.Errorf("app: unknown mailer backend %q", cfg.Backend)
	}
}

// Close stops goauth, closes the delivery connection and releases the cache and the database pool.
func (c *Core) Close() {
	_ = c.Auth.Close()
	_ = c.deliveryConn.Close()
	c.Cache.Close()
	c.Pool.Close()
}
