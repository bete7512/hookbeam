# hookbeam — AI working guide

## What this is

Hookbeam, an open-source webhook delivery platform (`product/README.md` is the spec). Go 1.26 + Postgres 16, module `github.com/bete7512/hookbeam`, one Go module.
Spec-first OpenAPI (oapi-codegen, std `net/http` ServeMux), hand-written pgx repos, goose Go migrations.
Services live at the repository root, one Go module. `api/` is the control plane (identity, workspaces, API keys, public REST; identity and workspaces are goauth, mounted at `/v1/auth`: signup, login, sessions, verification, reset, Google, organizations; plus health, `/v1/me`, workspace API keys, the outbox and worker): two binaries, `api/cmd/api` (HTTP) and `api/cmd/worker` (outbox relay, outbox cleanup, event handlers over SNS/SQS), share its packages (`app`, `config`, `services`, `repos`, `models`, `events`, `jobs`). `delivery/` is the data plane (environments, applications, endpoints, signing secrets, subscriptions, messages, deliveries, attempts): `delivery/cmd/server` (gRPC over mTLS on `:9090`, own database `hookbeam_delivery`) and `delivery/cmd/worker` (claims due deliveries, signs and posts them, records attempts; relays and prunes the delivery outbox) share `app`, `config`, `services`, `repos`, `models`, `rpc`, `sender`, `events`, `migrations`. The two planes talk only through the gRPC contract in `contracts/` (`hookbeam.delivery.v1`, buf-generated Go in `contracts/gen/go`) and through events. `console/` (Next.js) is planned, not built. Makefile is the only entrypoint.

## Commands

`make` loads `.env` if present (else defaults matching `.env.example`). Host Postgres port defaults to **5433**; one local instance holds two databases, `hookbeam` (control plane, compose's `POSTGRES_DB`) and `hookbeam_delivery` (data plane; `db-up` creates every name in `LOCAL_DATABASES` when missing, fresh or existing volume). In production each plane gets its own database from Terraform, never from the services.
Each service's main binary runs its migrations itself: `api -m migrate <goose cmd>`, `server -m migrate <goose cmd>` (`up`, `down`, `status`, `up-to N`…).
`APP ?= api` (and `SVC := $(APP)`) selects the service for per-service targets; `MAIN` is its request-serving binary (`api` → `cmd/api`, `delivery` → `cmd/server`): `make migrate-up APP=delivery`, `make build APP=delivery`, `make run-worker APP=delivery`. `SERVICES` (`api delivery`) plus `contracts` are the folders `gen-check` hashes.

| Target | Does |
|---|---|
| `setup` | install oapi-codegen, mockgen, air, golangci-lint, govulncheck, buf, protoc-gen-go, protoc-gen-go-grpc; create `.env` with generated keys; `certs` |
| `certs` | `scripts/certs`: local CA, server and client certificates for mTLS between the planes into `deploy/certs/` (git-ignored; skips when present, `-force` regenerates) |
| `db-up` / `db-down` / `db-reset` | compose `core` profile Postgres: start+wait, then `CREATE DATABASE` for each missing name in `LOCAL_DATABASES` / stop / wipe volume |
| `migrate-up` / `migrate-down` / `migrate-status` | `go run ./$(APP)/cmd/$(MAIN) -m migrate up\|down\|status` against that service's database |
| `run-api` / `dev` | `go run ./api/cmd/api` on `:8080` / `db-up` then `run-api` |
| `run-delivery` | `go run ./delivery/cmd/server`: gRPC on `:9090` with mTLS (needs `make certs`) |
| `run-worker` | `go run ./$(APP)/cmd/worker`: api: relay + cleanup + subscriber; delivery: dispatch loop (claim, sign, post, record) + wake-up subscriber + relay + cleanup; until SIGTERM |
| `run-receiver` | `go run ./examples/receiver`: demo receiver on `:8081/hooks` that verifies signatures and dedupes; `RECEIVER_SECRET=whsec_...`; `?status=503` and `?delay=15s` steer its answer |
| `queue-up` / `queue-down` / `queue-logs` | compose `queue` profile: LocalStack (SNS+SQS, `:4566`, `deploy/localstack/init.sh` creates topic, queue, DLQ) and Mailpit (`:1025` SMTP, `:8025` UI) |
| `build` | `bin/$(APP)/$(MAIN)`, `bin/$(APP)/worker` (`bin/api/api`, `bin/api/worker`, `bin/delivery/server`, `bin/delivery/worker`) |
| `gen` | `gen-openapi` + `gen-proto` + `gen-mocks` |
| `gen-openapi` | bundle `$(APP)/openapi` → `$(APP)/gen/openapi/openapi.bundle.yaml`, then oapi-codegen |
| `gen-proto` | `cd contracts && buf lint && buf generate` → `contracts/gen/go/hookbeam/delivery/v1/*.pb.go` (committed) |
| `gen-mocks` | `go generate ./...` |
| `gen-check` | hashes generated files under the service folders, runs `make gen`, fails if anything changed (no git needed) |
| `lint` / `lint-openapi` / `fmt` / `vet` | golangci-lint / kin-openapi validate (`APP`) / gofmt+goimports / go vet |
| `test` | `go test -race ./...` (no Docker) |
| `test-integration` | same with `-tags integration`; testcontainers, or with `TEST_DATABASE_URL` each package creates and drops its own `test_<hash>` database on that server |
| `test-cover` / `tidy` / `vuln` | coverage html / go mod tidy / govulncheck |
| `image` / `images` | `docker build -f $(APP)/cmd/$(TARGET)/Dockerfile .` (`TARGET` defaults to `MAIN`; `make image TARGET=worker`, `make images APP=delivery`) / both binaries; tags `hookbeam-<app>-<target>:<git sha>` |

## Layout

A service = a business area that owns its data; its processes (api, worker) live inside it.

```
api/                        control plane; delivery/ (data plane) copies this shape
  cmd/api/main.go                    HTTP process: config → app.NewCore → handlers → server (or -m migrate)
  cmd/<target>/Dockerfile            image for that binary; build context is the repo root
  cmd/worker/main.go                 background process: config → app.NewCore → errgroup of publishEvents (Events.Publish every OUTBOX_POLL_INTERVAL), cleanOutbox (Events.CleanOutbox hourly), Subscriber.Run(jobs.Handle) until SIGTERM
  app/core.go                        composition root shared by both binaries: pool, Tx, repos, cache, limiter, publisher, subscriber, mailer, goauth (initialized), services, delivery client
  app/delivery.go                    newDeliveryConn: lazy grpc.ClientConn over mTLS from DELIVERY_CLIENT_*; deliveryPinger answers /readyz through the health service
  app/authn.go                       NewAuth(AuthDeps) builds goauth over the pool (GORM storage, modules, config) without initializing it; MigrateAuth applies its migrations
  app/workspaces.go                  memberships: services.Memberships over goauth's organization storage (goauth role of a user in a workspace; the members of a workspace)
  config/                            app config: embeds pkg/config.Config, adds Auth (AUTH_*, all for goauth), Events (EVENTS_*, type from pkg/config), Outbox (OUTBOX_*), Mailer (MAILER_*), Cache (CACHE_*), OIDC (OIDC_GOOGLE_*), Delivery (DELIVERY_CLIENT_ADDR, _SERVER_NAME, _TIMEOUT, _TLS_{CERT_FILE,KEY_FILE,CA_FILE,INSECURE}); consts AuthBasePath, SessionName, AccessCookie
  handlers/                          HTTP only; imported only by cmd/api: environments.go (list, create, create application in one), authn.go (spec-driven auth: goauth sessions and workspace API keys with scopes, principal/actor helpers), orgpolicy.go (guardOrgRoute: the membership policy in front of goauth's invite, role-change and remove routes), members.go (list, set developer/viewer), health.go (/readyz pings the database and the delivery service), me.go, api_keys.go, applications.go, endpoints.go, messages.go (SubmitMessage forwards the raw body; GetMessage), deliveries.go (attempts, cancel, replay), router.go (mounts goauth's routes and merges its OpenAPI document)
  {services,repos,models}/           shared by api and worker (+ repos/repo_mocks, services/service_mocks); repos/outbox.go, processed_events.go own the plumbing tables, repos/api_keys.go the sender credentials, repos/member_roles.go the developer/viewer refinement of goauth members, repos/endpoint_health.go the health projection (`ObserveEndpointHealth` upsert: success resets the streak, a counted failure extends it, older observations are ignored); services/delivery.go is the data plane behind the gRPC client (scope metadata, FromStatus, proto→models/delivery.go; every mutation checks a capability first); services/authz.go (Authorizer: effective role + the spec's capability matrix), services/members.go, services/orgpolicy.go (rules in front of goauth's member routes); identity and organization tables belong to goauth
  events/                            event type constants + payload structs: the control plane's own copy of the data plane's delivery state events (`delivery.succeeded|failed|cancelled|retry_scheduled`, `DeliveryStatePayload`); account emails are goauth's, not outbox events
  jobs/                              worker only: jobs.New(Deps{Events, Logger, HandlerTimeout, Handlers}) → Jobs dispatching the Handlers map through Handle (EventService.ProcessOnce per event); `NewDeliveryState(EndpointHealth)` is registered for every delivery state type
  migrations/                        goose Go migrations NNNN_name.go + migration.go registry; migrations.Up(ctx, pool); 0001 baseline (updated_at trigger, outbox, processed_events), 0002 api_keys, 0003 endpoint_health (projection of delivery state events), 0004 member_roles, 0005 api_keys.application_id, 0006 audit_events, 0007 notifications + notification_preferences; goauth migrates its own tables from `api -m migrate up`
  openapi/openapi.yaml               root: info, tags, security, paths: $ref ./paths/<file>.yaml#/<key>
  openapi/paths/<resource>.yaml      path items + that resource's schemas/params in a local components: block
  openapi/components/*.yaml          shared only: Error, error responses
  gen/openapi/                       generated: openapi.gen.go, openapi.bundle.yaml (committed, never edit)
delivery/                   data plane; same shape, gRPC instead of HTTP, own database
  cmd/server/main.go                 gRPC process: config → app.NewCore → rpc.New → grpc.NewServer(TLS creds, grpcx.ServerInterceptors) + health → GracefulStop on SIGTERM (or -m migrate)
  cmd/worker/main.go                 background process: errgroup of dispatchDue (Dispatch.Dispatch now, on every wake-up and every DELIVERY_CLAIM_INTERVAL, claiming until a batch comes back empty; `Dispatch.Drain()` after the group exits), sweepLeases (Dispatch.Sweep every DELIVERY_CLAIM_SWEEP_INTERVAL), the wake-up subscriber (every queue event just signals the loop), expireRetention (Messages.ExpirePayloads + ExpireIdempotencyKeys every DELIVERY_PAYLOAD_EXPIRY_INTERVAL), publishEvents, cleanOutbox
  cmd/<target>/Dockerfile            image for that binary; build context is the repo root
  app/core.go                        pool, Tx, repos, secrets.Cipher, transport, safehttp client → sender, ApplicationService, EndpointService, MessageService, DispatchService, EventService
  config/                            App (APP_*), Log (LOG_*) shared; everything else under DELIVERY_: DATABASE_URL + DB_*, GRPC_{ADDR,SHUTDOWN_TIMEOUT}, TLS_{CERT_FILE,KEY_FILE,CA_FILE,INSECURE}, EVENTS_*, OUTBOX_*, SECRETS_{KEY,KEY_ID}, CLAIM_{INTERVAL 5s,BATCH_SIZE 20,LEASE_TTL 60s,SWEEP_INTERVAL 30s,ENDPOINT_CONCURRENCY 2,CONCURRENCY 10}, SENDER_{TIMEOUT 10s,ALLOWED_PORTS 443,8443,ALLOW_PRIVATE,MAX_RESPONSE_BYTES,MAX_PREVIEW_BYTES}, PAYLOAD_{MAX_BYTES 256KiB,RETENTION 168h,IDEMPOTENCY_WINDOW 168h,EXPIRY_INTERVAL 10m,EXPIRY_BATCH_SIZE 500}
  rpc/                               gRPC only; imported only by cmd/server: Server implements deliveryv1.DeliveryServiceServer over services; scope from grpcx.ScopeFromContext; errors through grpcx.Status; proto⇄model mapping
  sender/                            services.Sender over the safehttp client: POST, classified transport failures (timeout, dns, connection, tls, blocked_destination), bounded body preview, response headers minus set-cookie; redirects are recorded as their 3xx status
  events/                            DeliveryDue = "delivery.due" {message_id}: the wake-up the outbox carries; DeliverySucceeded/Failed/Cancelled/RetryScheduled with DeliveryStatePayload: what the control plane projects; DeliveryStateEvent(state) picks the type
  {services,repos,models}/           EnvironmentService (create development|production, list), ApplicationService (create in a given environment, or in the workspace's development one, created on first use), EndpointService (create = endpoint + encrypted secret + subscriptions in one tx; pause; resume; archive = archived_at + cancel unsent cycles in one tx; update = url/description/subscriptions with a revision bump; rotate = new secret version + retire_at on the others in one tx; every URL passes the sender's `safehttp.Config` policy, so private or http destinations are 422 unless `DELIVERY_SENDER_ALLOW_PRIVATE`), MessageService (Accept: idempotency, fan-out, deliveries, wake-up in one tx; GetMessage; ListAttempts; CancelDelivery; ReplayDelivery; ExpirePayloads / ExpireIdempotencyKeys batch loops), DispatchService (claim → load → sign → send → attempt + outcome in one fenced tx), Policy (spec's retry table) and Sender port, EventService; repos applications, endpoints (+ GetEndpointByID, ListSubscribedEndpoints), endpoint_secrets (+ ListActiveEndpointSecrets, RetireEndpointSecrets), subscriptions, messages, deliveries (ClaimDueDeliveries, RecordOutcome, ReleaseDelivery, ClaimExpiredLeases, CancelDelivery, FindReplay, CancelDeliveriesForEndpoint, ExpireDeadlines), attempts, outbox, processed_events (+ repo_mocks, service_mocks)
  migrations/                        0001 baseline: applications, endpoints, endpoint_secrets, subscriptions, messages (payload BYTEA: exact submitted bytes), deliveries, attempts, outbox, processed_events (decision 0010 state model); 0002 partial index on in-flight deliveries per endpoint; 0003 partial indexes on payload and idempotency-key expiry; 0004 environments (backfills one development environment per workspace, moves its applications under it, application names unique per environment); 0005 explorer indexes (application + event type, delivery message + state); 0006 audit_events; 0007 applications.response_previews_enabled; 0008 endpoint_health + health_episodes (decision 0018); 0009 deliveries.workspace_id (backfilled, default ''); 0010 partial index `deliveries_workspace_active_idx` for the backlog count
contracts/                  the inter-service contract, own folder, same module
  buf.yaml, buf.gen.yaml             buf v2, local protoc-gen-go + protoc-gen-go-grpc plugins
  proto/hookbeam/delivery/v1/        delivery.proto: DeliveryService (CreateApplication, CreateEndpoint, PauseEndpoint, AcceptMessage, GetMessage, ListAttempts, ListMessages, ListAuditEvents, GetMessagePayload, UpdateApplication, GetEndpoint, GetHealthEpisode, GetWorkspaceLimits, CancelDelivery, ReplayDelivery, UpdateEndpoint, RotateEndpointSecret, ResumeEndpoint, ArchiveEndpoint, CreateEnvironment, ListEnvironments) + messages; scope travels as metadata
  gen/go/hookbeam/delivery/v1/       generated (committed, never edit); import as deliveryv1
pkg/apperr            sentinels + AppError
pkg/config            common env → Config (App, HTTP, Log, DB), validated at boot; App/Log/Events/Outbox each have Validate so a service can embed only what it needs
pkg/db                pool (+retries), DBTX, WithTx, Transactor/NewTransactor, HandleError
pkg/events            Event envelope, Publisher, Subscriber, Handler; adapters memory/ (Bus), sns/ (NewClient, Publisher) and sqs/ (NewClient, Subscriber, WithLogger, WithVisibilityTimeout); transport/ New(ctx, config.Events, log) picks memory or aws
pkg/grpcx             gRPC plumbing: TLS{CertFile,KeyFile,CAFile,Insecure} with Validate/ServerCredentials (requires client certs)/ClientCredentials; Status(err) maps apperr → codes (+ BadRequest details for validation) and FromStatus maps back (details → validation fields; Unavailable/DeadlineExceeded → 503); Scope + WithScope/ScopeFromContext over metadata x-hookbeam-workspace-id / x-hookbeam-actor-id / x-request-id; ServerInterceptors(log) = request id, one "grpc request" log line, panic recovery, requireScope (health exempt); ClientInterceptor forwards the request id; RegisterHealth, Ping
pkg/webhooks          Standard Webhooks: Sign(secret, id, ts, body) → "v1,<base64>", Verify(secrets, headers, body, now, tolerance) accepting several signatures, Key(whsec_) ; header names webhook-id / webhook-timestamp / webhook-signature; the spec's test vector is in the tests
pkg/safehttp          Config{Timeout, AllowedPorts, AllowPrivate}.NewClient(): resolve-then-dial that refuses private, loopback, link-local, multicast, metadata and IPv4-mapped forms (Blocked), no redirects (ErrRedirect), no proxy, bounded timeouts; ValidateURL checks scheme (https; http only with AllowPrivate), credentials and port allowlist
pkg/certs             Generate(dir, hosts, validity) → local CA + server + client certificates (P-256); Paths, Files.Exist; used by scripts/certs and tests
pkg/secrets           AES-256-GCM Cipher: New(base64 key), Encrypt (random nonce), Decrypt
pkg/httpx             server, middleware, WriteJSON/WriteError/DecodeJSON, /docs, 404
pkg/logger            slog builder, request-id/context helpers, NewGoAuth (slog behind goauth's logger interface)
pkg/mailer            Message, Mailer; adapters memory/, smtp/ (Mailpit locally), ses/; NewGoAuthSender (Mailer behind goauth's EmailSender)
pkg/cache             Cache over ristretto: New(Config{MaxItems}), Get, Set(key, value, ttl), Wait, Close; one instance per app, built in Core
pkg/ratelimit         Limiter{Allow(ctx, key, limit, window) Decision}, Window, Decide; memory/ Limiter counts in a cache.Cache and is also goauth's RateLimitStore (Take, Reset, Close)
pkg/sorting           Columns: sort-key whitelist → OrderBy / Validate
pkg/testutil          Postgres(t, migrate) (integration tag; nil migrate = no migrations), Truncate, Main, Ptr, MustEnv
scripts/openapi-bundle/  shared tool, -in/-out per app
scripts/certs/        make certs: -dir deploy/certs -hosts localhost,127.0.0.1,delivery -validity 8760h -force
scripts/setup-env/    fills AUTH_SECRET_KEY, AUTH_ENCRYPTION_KEY, AUTH_JWT_PRIVATE_KEY, DELIVERY_SECRETS_KEY in .env
examples/receiver/    demo receiver (FR-24): verifies signatures with pkg/webhooks, dedupes by webhook-id in memory, ?status= and ?delay= steer it; `make run-receiver`
deploy/localstack/init.sh  creates topic hookbeam-events, queue hookbeam-worker (raw delivery), DLQ hookbeam-worker-dlq (maxReceiveCount 3)
deploy/certs/         local mTLS material from `make certs` (git-ignored)
docs/decisions/       one short file per architecture decision (context, options, decision, consequences) + README index; a changed decision is a new file
go.mod                one module for api/, delivery/, contracts/, pkg/ (decision 0002)
```

Import paths: `github.com/bete7512/hookbeam/api/<pkg>`, `.../delivery/<pkg>`, `.../contracts/gen/go/hookbeam/delivery/v1` (alias `deliveryv1`), `.../pkg/<pkg>`.

Planned, absent: `console/`, `infra/`, ECS task definitions.

## Services and split-readiness

1. A service imports only its own packages, `pkg/` and `contracts/`. Cross-service imports are a convention, not compiler-enforced (no `internal/`); `pkg/` importing a service is blocked by the `depguard` rule in `.golangci.yml` (deny list: `api`, `delivery`, `console`).
2. Each service owns its migrations, database URL, OpenAPI spec, generated code and config.
3. Services talk only through contracts (the gRPC client in `contracts/gen/go` or events); never shared tables or Go structs. The control plane dials the data plane over mTLS (`pkg/grpcx.TLS`; client and server certificates from one local CA, `make certs`); plaintext only with `*_TLS_INSECURE=true`, which staging/prod refuse. Every call carries the workspace (and the acting user) as metadata; the server's `requireScope` interceptor rejects calls without one and every repo read is scoped by `workspace_id`.
4. `pkg/` stays domain-free: `testutil.Postgres(t, migrate)` takes the service's migrate func; `pkg/config` holds only common settings, service settings live in `<service>/config`.
5. New service = copy the `api/` (HTTP) or `delivery/` (gRPC) folder shape to a new root folder, add it to `SERVICES` and `MAIN_<name>` in the Makefile and to the depguard deny list. A new business area with its own data is never a folder inside an existing service.

## Request flow

1. `httpx.Default`: `RequestID` → `Logger` (puts logger in ctx) → `Recover` → `MaxBody`.
2. Generated `openapi.HandlerWithOptions` binds path/query params; bind failure → `ErrorHandlerFunc` → 400 `BAD_REQUEST`.
3. Auth middleware (`handlers/authn.go`) enforces the op's spec `security`, puts claims in ctx (see Authentication).
4. Handler decodes body into generated DTO, maps to model, calls service.
5. Service validates, calls repo interface, classifies repo errors into `*apperr.AppError`.
6. Repo runs SQL via `db.DBTX`; driver errors → `db.HandleError` → wrapped sentinels.
7. Handler writes `httpx.WriteJSON` or `httpx.WriteError(w, r, err)`; `WriteError` records err for the access log line.

## Layer rules

| Layer | Owns | May import | Must not |
|---|---|---|---|
| `handlers` | implement `openapi.ServerInterface` with raw `(w, r, params)`; DTO⇄model mapping (`toUserDTO`) | `api/gen/openapi`, `models`, `services` (interfaces), `pkg/httpx`, `pkg/apperr`, goauth `pkg/types` (`PrincipalFromContext`) and `pkg/config` (`RouteInfo`); `repos` **only** for `ListXOpts` | SQL, pgx, status mapping, logging, token parsing |
| `services` | validation (`apperr.NewValidation`), business rules, repo-error classification; `EventService` publishes and prunes the outbox | `models`, `repos` (consume `repos.XRepo` directly), `pkg/apperr`, `pkg/events` (interfaces) | HTTP, DTOs, SQL, logging (keep `Logger` in Deps, unused), loops |
| `repos` | hand-written SQL, `XSortColumns` whitelist; the only place SQL lives (also `outbox`, `processed_events`) | `models`, pgx, `pkg/db`, `pkg/apperr`, `pkg/sorting` | business rules, HTTP, logging |
| `models` | structs with `db:"col"` tags (also row types) | stdlib | everything else |
| `events` | event type constants + payload structs | stdlib | everything else |
| `jobs` | `Jobs`: handler methods over `Deps`, listed in the map in `New`, dispatched by `Handle`; may log | `services` (interfaces), `pkg/events`, `pkg/mailer`, `events` | SQL, `repos`, pgx, HTTP, `handlers` |
| `app` | `Core`: pool, `Tx`, repos, cache, limiter, goauth, services, publisher, subscriber, mailer, delivery client | `pkg/*`, `repos`, `services`, goauth, `contracts/gen/go` | `handlers` (api-only) |
| `rpc` (delivery) | implement `deliveryv1.DeliveryServiceServer`; proto⇄model mapping; scope from `grpcx.ScopeFromContext`; every error through `grpcx.Status` | `contracts/gen/go`, `models`, `services` (interfaces), `pkg/grpcx` | SQL, `repos`, status codes by hand, logging (the interceptor logs), metadata parsing (the interceptor does it) |

- `api/handlers` is imported only by `api/cmd/api`; `api/app` never imports handlers. `delivery/rpc` is imported only by `delivery/cmd/server`.
- Delivery services take a `services.Scope{WorkspaceID, ActorID}` as their first argument after `ctx` and return `apperr.NewUnauthorized` when the workspace is empty; repos take `workspaceID` on every read so a row of another workspace is `ErrNotFound`, never `Forbidden` (no existence leak).

Ids: `BIGINT GENERATED ALWAYS AS IDENTITY` in SQL, `int64` in Go at every layer, `type: integer, format: int64, minimum: 1` in the spec (decision 0008).

Handler shape:
```go
var req openapi.CreateUserRequest
if err := httpx.DecodeJSON(r, &req); err != nil { httpx.WriteError(w, r, err); return }
user := &models.User{Email: req.Email, Name: req.Name}
if err := h.deps.Users.Register(r.Context(), user, req.Password); err != nil { httpx.WriteError(w, r, err); return }
_ = httpx.WriteJSON(w, http.StatusCreated, toUserDTO(user))
```

Repo rules (`api/repos/<resource>.go`):
- Exported `XRepo` interface + `ListXOpts` + unexported struct over `db.DBTX`; `NewXRepo(conn db.DBTX) (XRepo, error)`.
- Explicit column-list consts (`userColumns`, `userListColumns`); never `SELECT *`.
- `pgx.NamedArgs` (`@name`); reads via `pgx.CollectOneRow` / `pgx.CollectRows` with `pgx.RowToStructByName[models.X]`.
- Every error returned through `db.HandleError(err)` (NoRows → `ErrNotFound`, 23505 → `ErrConflict`).
- `Exec` with `RowsAffected() == 0` → return `apperr.ErrNotFound`.
- Writes take `*models.X`, fill generated fields via `RETURNING` (Scan or CollectOneRow into the pointer), return only `error`.
- Never set `updated_at`; the trigger does.

## Constructors & wiring

- One `XDeps` struct; `NewX(deps XDeps) (X, error)`; single `if deps.A == nil || deps.B == nil` check returning `errors.New`.
- Services/repos return the interface; `handlers.New` returns `*Handler`. No DI framework.
- `api/app/core.go` is the service's shared composition root: `app.NewCore(ctx, app.Deps{Config, Logger})` → `*app.Core{Pool, Auth, APIKeys, Events, Cache, RateLimiter, Publisher, Subscriber, Mailer, Delivery, DeliveryHealth}` (`Auth` is the initialized `*goauth.Auth`; `Delivery` is a `deliveryv1.DeliveryServiceClient` over a lazy mTLS connection from `DELIVERY_CLIENT_*`, `DeliveryHealth` an `app.Pinger` for `/readyz`; repos stay inside `NewCore`); `Close()` shuts goauth down, closes the delivery connection and releases the cache and the pool.
- `NewCore` builds `outboxRepo`, `processedRepo`, `tx := db.NewTransactor(pool)`, the cache (`cache.New(CACHE_MAX_ITEMS)`) and `ratelimitmemory.New(cache)`, the events adapters through `transport.New` (`EVENTS_BACKEND`: `memory` bus or `aws` SNS/SQS), the mailer (`MAILER_BACKEND`: `memory`, `smtp`, `ses`), then goauth through `NewAuth(AuthDeps{Config, Pool, Mailer, Limiter, Logger})` + `Initialize`, then `APIKeyService` (Keys, Memberships over `authn.Config().Storage.Organization()`, Logger) and `EventService` (Tx, Outbox, Processed, Publisher, BatchSize, Retention).
- `app/authn.go` `NewAuth`: GORM over the same pgx pool (`stdlib.OpenDBFromPool`, `BorrowStorage: true`), `BasePath: config.AuthBasePath`, our slog behind `logger.NewGoAuth`, the limiter as `RateLimit.Store`, `TrustedProxies` from `HTTP_TRUST_PROXY`, `Migration.Auto: false`, `FrontendConfig` from `AUTH_LINK_BASE_URL` (`/login`, `/signup`, `/reset-password`, `/verify-email`); modules `stateless` (refresh rotation), `notification` (our mailer behind `mailer.NewGoAuthSender`, Hookbeam branding, welcome email), `organization` (`AutoCreateOrg`: signup creates "<name>'s workspace"; invitations land on `AUTH_LINK_BASE_URL/invitations/accept`), `oauth` with Google when `OIDC_GOOGLE_CLIENT_ID` is set. The `audit` module is not registered: its routes require the `admin` module.
- Config: `api/config` embeds `pkg/config.Config` (App, HTTP, Log, DB) and adds `Auth` (`AUTH_JWT_ALGORITHM` ES256|RS256|HS256, `AUTH_JWT_PRIVATE_KEY` base64 PKCS#8 PEM or `AUTH_JWT_PRIVATE_KEY_FILE`, `AUTH_JWT_KEY_ID`, `AUTH_SECRET_KEY` ≥32 chars, `AUTH_ENCRYPTION_KEY` ≥32 chars and different, `AUTH_ISSUER`, `AUTH_AUDIENCE`, `AUTH_ACCESS_TOKEN_TTL` 15m, `AUTH_REFRESH_TOKEN_TTL` 720h, `AUTH_LINK_BASE_URL`, `AUTH_API_URL`, `AUTH_ALLOW_INSECURE_COOKIES` local only), `Events` (`EVENTS_BACKEND`, `EVENTS_AWS_REGION`, `EVENTS_AWS_ENDPOINT_URL`, `EVENTS_TOPIC_ARN`, `EVENTS_QUEUE_URL`; AWS creds from `AWS_*`), `Outbox` (`OUTBOX_POLL_INTERVAL` 500ms, `OUTBOX_BATCH_SIZE` 100, `OUTBOX_RETENTION` 168h), `Mailer` (`MAILER_BACKEND`, `MAILER_SMTP_ADDR`, `MAILER_FROM`), `Cache` (`CACHE_MAX_ITEMS` 100000), `OIDC` (`OIDC_GOOGLE_CLIENT_ID`, `_SECRET`; empty id = no Google provider). Validation parses the PEM and checks the curve/size against the algorithm; staging/prod refuse HS256, insecure cookies and non-public origins. `HTTP_TRUST_PROXY` feeds goauth's `TrustedProxies`. `scripts/setup-env` generates the three keys.
- `api/cmd/api/main.go`: config → logger → `app.NewCore` → `core.Auth.GenerateOpenAPIDocs` → `handlers.New(handlers.Deps{Auth: core.Auth, APIKeys: core.APIKeys, DB: core.Pool, Delivery: core.DeliveryHealth, AccessCookie: config.AccessCookie, Logger})` → `handlers.NewRouter(h, RouterOptions{Logger, MaxBodyBytes, AuthRoutes: core.Auth.Routes(), AuthSpec, AuthBasePath})` → server. `-m migrate <cmd>` runs goose, then on `up` builds goauth (memory mailer, throwaway cache) and applies its migrations; `status` lists both.
- `api/cmd/worker/main.go`: config → logger → `app.NewCore` → `j := jobs.New(jobs.Deps{Events, Logger, HandlerTimeout, Handlers: map[string]jobs.Handler{...}})` → errgroup of `publishEvents` (calls `Events.Publish` now and every `OUTBOX_POLL_INTERVAL`), `cleanOutbox` (calls `Events.CleanOutbox` now and hourly) and `core.Subscriber.Run(gctx, j.Handle)` → SIGTERM cancels. The two loops are plain ticker functions in `main.go` that log a warning on failure; services never loop or log.

- `delivery/app/core.go`: `app.NewCore(ctx, app.Deps{Config, Logger})` → `*app.Core{Pool, Applications, Endpoints, Messages, Dispatch, Events, Publisher, Subscriber}`: pool with retries, the nine repos, `tx`, `secrets.New(DELIVERY_SECRETS_KEY)`, `transport.New`, `safehttp.Config{...}.NewClient()` → `sender.New`, then `ApplicationService`, `EndpointService{Tx, Applications, Endpoints, Deliveries, Secrets, Subscriptions, Cipher, KeyID, URL: safehttp.Config, Logger}`, `MessageService{Tx, Applications, Endpoints, Messages, Deliveries, Attempts, Outbox, Policy: DefaultPolicy, PayloadRetention, IdempotencyWindow, MaxPayloadBytes, ExpiryBatchSize, Logger}`, `DispatchService{Tx, Deliveries, Attempts, Messages, Endpoints, Secrets, Cipher, Sender, Policy, LeaseTTL, BatchSize, EndpointConcurrency, Concurrency, Logger}`, `EventService`. `delivery/cmd/server/main.go`: config → logger (service `delivery`) → `NewCore` → `cfg.TLS.ServerCredentials()` → `rpc.New(rpc.Deps{Applications, Endpoints, Logger})` → `grpc.NewServer(grpc.Creds, grpcx.ServerInterceptors(log))` + `RegisterDeliveryServiceServer` + `grpcx.RegisterHealth` → `Serve`; SIGTERM → health `Shutdown`, `GracefulStop` bounded by `DELIVERY_GRPC_SHUTDOWN_TIMEOUT`. `-m migrate <cmd>` runs goose against `DELIVERY_DATABASE_URL`.
- Adding an RPC: extend `contracts/proto/hookbeam/delivery/v1/delivery.proto` → `make gen-proto` → implement the method on `rpc.Server` (the embedded `UnimplementedDeliveryServiceServer` answers `Unimplemented` until then) → service → repo → `rpc/server_test.go` case over bufconn with `grpcx.WithScope`. On the control plane add a method to `services.DeliveryService` (`api/services/delivery.go`: `s.call(ctx, workspaceID, actorID)` scopes and bounds the context, `grpcx.FromStatus` maps the error, `to*` maps proto into `api/models/delivery.go`), a mock case in `service_mocks`, then the handler. Handlers never see proto types.
- A control-plane handler backed by the data plane (`applications.go`, `endpoints.go`, `messages.go`, `deliveries.go`) is thin: `actor(r)` or `principal(r)`, decode, one `h.deps.Delivery` call, write. Validation and business rules live in the data plane and come back as `apperr` errors with their fields.

Wiring a new resource `Project`:
1. `api/app/core.go`: add `Projects services.ProjectService` to `Core`.
2. `core.go`: `projectRepo, err := repos.NewProjectRepo(pool)`, then `services.NewProjectService(services.ProjectServiceDeps{Projects: projectRepo, Logger: d.Logger})` → `Core.Projects`.
3. `api/handlers/handler.go`: add `Projects services.ProjectService` to `Deps` and to the nil check in `New`.
4. `api/cmd/api/main.go`: pass `Projects: core.Projects` in `handlers.Deps{...}`; update `newFixture` in `handler_test.go`; add `projects_test.go` with `runEndpoints(t, []endpointCase{...})`.
5. Worker needs it: read `core.Projects` in `api/cmd/worker/main.go`.

## Delivery flow (data plane)

- Quotas (issue 20): `services.Limits` (from `DELIVERY_LIMITS_{ENDPOINTS_PER_APPLICATION 20, FANOUT_PER_MESSAGE 20, BACKLOG_PER_WORKSPACE 10000, BACKLOG_RETRY_AFTER 30s}`) is a dep of the endpoint and message services. `CreateEndpoint` counts the application's unarchived endpoints (`CountActiveEndpoints`) and answers 422 on field `endpoints` at the cap; `Accept` answers 422 on `type` when the fan-out exceeds `FanoutPerMessage` and 429 with `Retry-After` = `BacklogRetryAfter` when the workspace's non-terminal deliveries plus the fan-out would exceed `BacklogPerWorkspace` (`CountActiveDeliveries(ws, bound)`: a bounded count over `deliveries.workspace_id` and the partial index, so it never scans past the cap). Every check runs before the transaction, so a refused submission leaves no row. `Retry-After` crosses the planes as a `RetryInfo` status detail (`grpcx.Status`/`FromStatus`); a data plane that does not answer is 503 with `Retry-After: 5` (`grpcx.UnavailableRetryAfter`), never buffered. `GET /v1/workspaces/current/limits` (sessions, every role) → `GetWorkspaceLimits` RPC → `LimitService.Usage`: the limits, the backlog now and every application's active endpoint count.
- Health episodes (issue 18, decision 0018): the data plane keeps one `endpoint_health` row per endpoint (streak, highest revision observed, last state, observed_at) and `health_episodes` (one open per endpoint, partial unique index). `services.HealthService.Observe(ctx, tx, obs)` runs inside the dispatch outcome transaction (`record` only; a lost attempt or a cancellation observes nothing): the upsert locks the row and ignores observations under an older revision; the third consecutive failure opens an episode and writes `endpoint.health_changed {state: unhealthy}` to the outbox, the next success resolves it and announces `healthy`; a further failure only extends the open episode. `HealthService.Status` attaches `models.HealthStatus{State unknown|healthy|unhealthy, ObservedAt, Stale (> 15 min), ConsecutiveFailures}` to every endpoint response (`withEventTypes`); `GET /v1/endpoints/{id}` (sessions) reads one. The control plane worker consumes `endpoint.health_changed` with `jobs.NewEndpointHealthChanged` → `NotificationService.HandleHealthChange`.
- Notifications (issue 19): `notifications` (one row per episode, kind `failure|recovery` and recipient, unique index `notifications_once_idx`) and `notification_preferences` (`email_alerts` per user, no row = on). `services.NotificationService.HandleHealthChange` rereads the episode through `GetHealthEpisode` (a system read scoped by workspace, no actor), drops a failure event whose episode already resolved, and for every owner and admin (`Memberships.Members`) stores a notice in `Tx` and, when new and the recipient wants emails, writes `notification.email_requested {notification_id, user_id}` to the outbox in the same transaction; `jobs.NewNotificationEmail` → `SendEmail` mails it through `pkg/mailer` only when goauth says the address is verified (`services.Users.Contact`, implemented in `app/users.go` over goauth's core storage). Routes (sessions only): `GET /v1/notifications` (`limit`, `offset`, `unread`) → `{items, total, unread}`, `POST /v1/notifications/{id}/read` (idempotent), `GET|PUT /v1/me/notification-preferences {emailAlerts}`.
- Payloads (issue 16): `GET /v1/messages/{id}/payload` returns the retained bytes exactly as submitted (`content-type: application/json`), for sessions with `CapReadPayloads` (not viewers) or a key with `payloads:read`; the data plane's `GetMessagePayload` audits every permitted read (`message.payload_read`, ids and byte count only) in the same transaction and answers 410 `GONE` (`apperr.NewGone`, `codes.FailedPrecondition` on the wire) once the payload has expired. `PATCH /v1/applications/{id}` `{responsePreviewsEnabled}` (owners and admins, audited `application.update`) flips the application setting; `ListAttempts` carries `include_previews`, which the control plane sets from the role matrix (user) or the `payloads:read` scope (key), and the data plane keeps `response_preview` only when both that and the application setting hold. `DELIVERY_SENDER_MAX_PREVIEW_BYTES` defaults to 4096. Application create is audited too (`application.create`).
- Audit (issue 17): both planes have an `audit_events` table and `repos.AuditRepo` (same shape: workspace, actor, action, resource type and id, request id, JSONB details without secrets or payload bytes, occurred_at). The data plane writes a row inside the owning transaction through `services.recordAudit` for endpoint create/update/pause/resume/archive/rotate (`AuditEndpoint*`), delivery cancel and replay; the control plane for key create/revoke (`APIKeyService` now runs in `Tx`) and member role changes, and after a guarded goauth route succeeds (`AuditService.Record`, best effort, goauth's own transaction). `GET /v1/audit` (every role) merges the control plane's rows with the data plane's (`ListAuditEvents` RPC) newest first, each item tagged `source: control|delivery`; filters `actor_id`, `resource_type`, `resource_id`, `after`, `before`. Both workers prune rows older than `AUDIT_RETENTION` / `DELIVERY_AUDIT_RETENTION` (90 days) hourly.
- Environments (issue 14): `POST/GET /v1/environments` (`{name, kind: development|production}`, owners and admins create), `POST /v1/environments/{id}/applications`; `POST /v1/applications` stays and lands in the workspace's development environment (`EnsureDefaultEnvironment`). Application and message reads carry `environmentId` and `environmentKind` (the application repo joins environments; `MessageService.GetMessage` returns the application too).
- Submit: `POST /v1/applications/{id}/messages` with a key holding `messages:write` and an `Idempotency-Key` header (binding error 400 without it). The handler reads the raw body (`httpx.ReadBody`, 413 over `HTTP_MAX_BODY_BYTES`), `services.DeliveryService.SubmitMessage` reads `type` and `external_id` from it and forwards the exact bytes to `AcceptMessage`.
- Accept (`delivery/services/messages.go`): validate (`type` 1..100 no whitespace, key 1..255, body ≤ `DELIVERY_PAYLOAD_MAX_BYTES` valid UTF-8 JSON); application in the workspace and not archived; same key in the application → same SHA-256 and type returns the first result with `Duplicate`, anything else 409; `ListSubscribedEndpoints` (subscribed to the type, or no subscriptions at all; archived excluded, paused included); none → 422 on `type`; one transaction: `messages` row (`public_id` = `msg_` + 22 url-safe chars, the `webhook-id`; `payload` BYTEA; `idempotency_expires_at` = now + window; `payload_expires_at` = now + retention), one `deliveries` row per endpoint (`queued`, `due_at` now, `deadline_at` now + 24h, `endpoint_revision`), one outbox `delivery.due`. A unique-violation on the insert means a concurrent duplicate won: re-read and answer as a duplicate.
- Dispatch (`delivery/services/dispatch.go`, worker): one transaction takes `LockClaims` (a single advisory lock, so claims run one at a time across replicas) and `ClaimDueDeliveries(batch, DELIVERY_CLAIM_ENDPOINT_CONCURRENCY, uuid lease, DELIVERY_CLAIM_LEASE_TTL)`: due `queued`/`retry_scheduled` rows of unpaused, unarchived endpoints, ranked per endpoint by `due_at` (`row_number` window), capped at `EndpointConcurrency − in_flight` per endpoint (index `deliveries_inflight_endpoint_idx`, migration 0002), ordered rank first so every endpoint's oldest delivery is taken before any second one, then moved to `in_flight` with `attempt_count + 1`; `Dispatch` returns as soon as the rows are claimed and each attempt runs in its own goroutine under `DELIVERY_CLAIM_CONCURRENCY` slots (a claim takes at most the free slots; zero free slots claims nothing), with a context detached from cancellation and bounded by the lease TTL, so a stalling destination never delays the next claim and a shutdown still records what was sent (`Drain` waits for them); per delivery: load message, endpoint (archived → `cancelled` with reason `endpoint_archived`, no attempt), active secrets (newest first; all sign, `webhook-signature` holds one `v1,…` per secret), decrypt, sign `${public_id}.${unix}.${body}` with HMAC-SHA256, POST through `services.Sender` (`content-type`, `user-agent: Hookbeam/1`, the three webhook headers), `Policy.Decide`, then one transaction: `CreateAttempt` (number = attempt_count, revision, secret version, status or error class, headers, bounded preview, latency) + `RecordOutcome` fenced by the lease token (lost lease → warning, nothing written). A failure before anything is sent (message, endpoint or secret load, signing) calls `ReleaseDelivery`: lease cleared, `attempt_count` undone, `due_at` = the lease's expiry, state back to `queued` or `retry_scheduled`.
- Sweep (`DispatchService.Sweep`, worker, every `DELIVERY_CLAIM_SWEEP_INTERVAL`): also `ExpireDeadlines(batch)`: `queued`/`retry_scheduled` rows past `deadline_at` become `failed` with `deadline_exceeded` (the claim query already skips them), so a cycle that aged out while paused is never attempted. `ClaimExpiredLeases(batch, uuid, LeaseTTL)` re-leases `in_flight` rows past `lease_expires_at` (`FOR UPDATE SKIP LOCKED`, returns `models.ExpiredDelivery` with `LeaseExpiredAt`); per row: `CreateAttempt` with `error_class` `unknown` (number = `attempt_count`, no response, `started_at` = lease expiry − TTL) + `RecordOutcome` from `Policy.Decide` on an unknown result (retry, or `attempts_exhausted`/`deadline_exceeded`), fenced by the new token. Overdue `queued`/`retry_scheduled` rows whose wake-up was lost need no sweep: the claim loop's timer picks them up.
- Policy (`services.DefaultPolicy`, the spec's): 2xx → `succeeded`; 408, 425, 429, 5xx and transport failures except `blocked_destination` retry; other 4xx and all 3xx → `failed` with `http_<code>`; delays 15s, 1m, 5m, 30m, 2h, 6h, 12h ±20% jitter after attempts 1–7, attempt 8 → `attempts_exhausted`; `Retry-After` (seconds or HTTP date) wins when later than the backoff; a next attempt past `deadline_at` → `deadline_exceeded`.
- State events: every outcome transaction (`record`, the archived-endpoint cancel, `CancelDelivery` of unclaimed work) also writes a `delivery.<state>` outbox event with `DeliveryStatePayload` (ids, attempt, state, reason, status code, error class, occurred_at); `RecordOutcome` returns the stored state so the event names what the row became. Bulk transitions (`CancelDeliveriesForEndpoint`, `ExpireDeadlines`) emit nothing yet. The control plane's worker handles them with `jobs.NewDeliveryState` → `EndpointHealthService.Observe` → `endpoint_health` (one row per endpoint: `consecutive_failures`, last outcome, `observed_at`; a success resets, a counted failure increments, cancellations, `deadline_exceeded` and `unknown` only refresh, an older event never overwrites a newer row).
- Queues: one SQS queue per worker, both subscribed to the one SNS topic with a filter on the `type` message attribute the publisher sets (`deploy/localstack/init.sh`: `hookbeam-worker` gets everything but `delivery.due`, `hookbeam-delivery-worker` only `delivery.due`; each with its own DLQ). The data plane reads `DELIVERY_EVENTS_QUEUE_URL`.
- Wake-ups: the worker claims on a timer (`DELIVERY_CLAIM_INTERVAL`) and whenever any event arrives on its queue; wake-ups carry no work, so duplicates are harmless (decision 0010). With `DELIVERY_EVENTS_BACKEND=memory` the relay and the subscriber share the in-process bus of the worker.
- Read: `GET /v1/messages/{id}` (message + deliveries), `GET /v1/deliveries/{id}/attempts`; sessions or keys with `messages:read`; every read is scoped by the message's `workspace_id`.
- Explorer (issue 15): `GET /v1/applications/{id}/messages` and `GET /v1/environments/{id}/messages` with `limit`, `offset`, `type`, `external_id`, `accepted_after`, `accepted_before`, `outcome` (a delivery state at least one delivery is in), `sort_by` (`id`, `acceptedAt`), `sort_dir` → `{items: [MessageSummary{message, deliveryCounts, payloadExpiresAt}], total}`; sessions or keys with `messages:read`. `ListMessages` RPC → `MessageService.ListMessages` (validates, checks the application or environment belongs to the workspace) → `MessageRepo.ListMessages`/`CountMessages` (`repos.ListMessagesOpts`, `MessageSortColumns`, one filter string, counts per state as scalar subqueries over `deliveries_message_state_idx`). Payloads are never listed. `rpc/` may import `repos` only for `ListXOpts`, like handlers.
- Cancel: `POST /v1/deliveries/{id}/cancel` (sessions only, `actor(r)`) → `CancelDelivery` → `DeliveryRepo.CancelDelivery`: `queued`/`retry_scheduled` become `cancelled` now (reason `cancelled`, `due_at` NULL, never claimed again); `in_flight` gets `cancel_requested` and the attempt settles first: `RecordOutcome` and `ReleaseDelivery` turn any non-success into `cancelled` in SQL, a 2xx still records `succeeded`. A finished delivery is returned unchanged (idempotent).
- Replay: `POST /v1/deliveries/{id}/replay` with `Idempotency-Key` (sessions only) → `ReplayDelivery`: the original must be `failed` or `cancelled`, the endpoint not archived, the payload retained; one transaction inserts a new `deliveries` row (`queued`, current `endpoint_revision`, `deadline_at` = now + 24h capped by `payload_expires_at`, `replay_of_delivery_id`, `replay_idempotency_key`) and a `delivery.due` wake-up. The same key returns the same cycle (`FindReplay`, also after a lost insert race); a different key while a cycle is active is 409 from `deliveries_active_cycle_idx`. The `webhook-id` stays the message's, so receivers dedupe.
- Retention (worker, every `DELIVERY_PAYLOAD_EXPIRY_INTERVAL`, `DELIVERY_PAYLOAD_EXPIRY_BATCH_SIZE` rows per batch): `ExpirePayloads` sets `payload` to empty bytes past `payload_expires_at` (metadata, deliveries and attempts stay; replay then answers 409 "payload has expired"); `ExpireIdempotencyKeys` nulls `idempotency_key` past `idempotency_expires_at`, so the same key is accepted again as a new submission. Rows are never deleted by these jobs.
- Secret rotation: `POST /v1/endpoints/{id}/rotate-secret` `{retireIn: "24h"}` (default 24h, `0s` = emergency, max 168h; sessions only) → `RotateEndpointSecret`: a new `endpoint_secrets` version (raw secret returned once) and `retire_at` on every other secret in one transaction (an earlier `retire_at` is kept). `ListActiveEndpointSecrets` filters `retire_at > now()`, so no retirement job is needed; the worker signs with every active secret, so the header carries two `v1,` signatures during the overlap and attempts record the newest version.
- Endpoint lifecycle: `PATCH /v1/endpoints/{id}` (url, description, eventTypes; any present field changes, `revision + 1`, subscriptions replaced in the same transaction; existing deliveries keep their planned revision and attempts record the revision and URL actually used), `POST /v1/endpoints/{id}/pause` | `/resume`, `DELETE /v1/endpoints/{id}` (archive), sessions only. Pause and resume flip `paused_at` and are idempotent; an archived endpoint answers 409 to both. Archive sets `archived_at` and cancels the endpoint's `queued`/`retry_scheduled` deliveries (`endpoint_archived`) in one transaction and reports the count; an in-flight attempt finishes and records normally; fan-out already excludes archived endpoints; archiving twice is a no-op.
- Destinations: `pkg/safehttp` refuses private, loopback, link-local, multicast, unspecified and metadata addresses after DNS resolution, follows no redirect, allows https on `DELIVERY_SENDER_ALLOWED_PORTS` (443, 8443); `DELIVERY_SENDER_ALLOW_PRIVATE=true` (dev only, refused in staging/prod) allows http and private addresses for the demo receiver on localhost.
- Local loop: `make run-delivery`, `make run-worker APP=delivery`, `make run-api`, create an application and an endpoint at `http://localhost:8081/hooks` (session), `RECEIVER_SECRET=<signingSecret> make run-receiver`, submit with the key; the receiver logs "verified and applied", `GET /v1/messages/{id}` shows `succeeded`. `?status=503` on the endpoint URL shows `retry_scheduled` with `due_at` about 15s later and a second attempt.

## Events and jobs

- Producing: inside the service's `Tx.WithTx` block, `deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.X, Payload: json})`. Same transaction as the row that caused it; never publish from a service or handler.
- Publishing (`services.EventService.Publish`): drains the outbox batch by batch, one transaction each: `ClaimOutboxEvents(limit)` (`FOR UPDATE SKIP LOCKED`) → `Publisher.Publish` → `MarkOutboxEventsPublished`; stops after a short batch and returns the total. A failed batch rolls back and the error is returned; the worker retries next tick.
- Pruning (`services.EventService.CleanOutbox`): `DeleteOutboxEventsPublishedBefore(now - OUTBOX_RETENTION, batch)` until a short batch; the worker calls it hourly.
- Consuming: `Subscriber.Run(ctx, j.Handle)`; `Jobs.Handle` looks up the handler by type and calls `EventService.ProcessOnce(ctx, e.ID, fn)`, which in one transaction runs `ProcessedEventRepo.MarkEventProcessed(id)` (`ON CONFLICT DO NOTHING`; `false` = replay, `fn` skipped) and then `fn`, the registered `jobs.Handler(ctx, event)`. Handler error → rollback of the mark → the queue redelivers (SQS: 3 receives, then the DLQ). Unknown type or invalid envelope → error, retained for configured DLQ redrive. Jobs have bounded contexts and SQS renews visibility while processing one message per sequential consumer.
- Handlers call services and ports (`services.EventService`, `mailer.Mailer`), never repos. Their own writes run outside the mark's transaction, so a handler is retried as a whole on failure and must tolerate that.
- Transport contract (`pkg/events`): `Publisher.Publish(ctx, []Event)`, `Subscriber.Run(ctx, Handler)`; nil from the handler acks, error leaves the message for redelivery. Any at-least-once broker with a dead-letter path can replace `pkg/events/sns` + `pkg/events/sqs`; one package per broker feature, never named after the vendor.
- Local: `make queue-up`, `EVENTS_BACKEND=aws`, `MAILER_BACKEND=smtp` (`.env.example` has the block); emails land in Mailpit at `http://localhost:8025`. `EVENTS_BACKEND=memory` keeps the outbox but the api process has no subscriber, so nothing is handled.
- New event: `add-job` skill.

## Errors (`pkg/apperr`)

| Constructor | Status | `error` code |
|---|---|---|
| `NewNotFound(resource, err)` | 404 | `RESOURCE_NOT_FOUND` |
| `NewConflict(msg, err)` | 409 | `ALREADY_EXISTS` |
| `NewValidation(fields map[string]string)` | 422 | `VALIDATION_FAILED` (+ `fields`) |
| `NewBadRequest(msg, err)` | 400 | `BAD_REQUEST` |
| `NewUnauthorized(msg, err)` / `NewForbidden(msg, err)` | 401 / 403 | `UNAUTHORIZED` / `FORBIDDEN` |
| `NewUnavailable(msg, err)` | 503 | `SERVICE_UNAVAILABLE` |
| `NewTooManyRequests(retryAfter)` | 429 | `RATE_LIMITED` (+ `Retry-After` header) |
| `NewGone(msg, err)` | 410 | `GONE` |
| `NewInternal(err)` | 500 | `INTERNAL_ERROR` (message is generic) |
| `httpx.DecodeJSON` body over limit | 413 | `PAYLOAD_TOO_LARGE` |

Sentinels: `ErrNotFound, ErrConflict, ErrValidation, ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrUnavailable, ErrRateLimited, ErrGone, ErrInternal`.

- Repo: return `db.HandleError(err)` or a bare sentinel; never an `AppError`.
- Service: `errors.Is(err, apperr.ErrX)` → `apperr.NewX(...)`; anything else → `apperr.NewInternal(err)`.
- Handler: pass any error to `httpx.WriteError`; non-`AppError` becomes 500 without leaking text.

```go
func userError(err error) error {
	if errors.Is(err, apperr.ErrNotFound) {
		return apperr.NewNotFound("user", err)
	}
	return apperr.NewInternal(err)
}
```

Body (`httpx.ErrorResponse`, `application/json`): `{"error":"RESOURCE_NOT_FOUND","message":"user not found","code":404,"fields":{...},"request_id":"..."}`.

Logging: `httpx.Logger` writes one `http request` line per request (Info < 500, Error ≥ 500) with `method, route (r.Pattern), path, status, duration_ms, error`. Never log-and-return; return the error.

## Authentication

Identity is [goauth](https://github.com/bete7512/goauth) v0.3.0-beta.1 (decision 0003): `api/app/authn.go` builds it, `Core.Auth` is the initialized `*auth.Auth`.

- goauth owns `users`, `tokens`, `sessions`, `accounts`, `organizations`, `organization_members` and `org_invitations`, with VARCHAR(36) ids. Hookbeam tables reference users and workspaces as strings (decision 0008). Never write SQL against goauth's tables.
- Routes are goauth's, mounted at `/v1/auth` from `Auth.Routes()` in `router.go`, outside the generated router and its middleware: `POST signup|login|refresh|logout|change-password|forgot-password|reset-password|send-verification-email|resend-verification-email|verify-email`, `GET me`, `PUT profile`, `GET is-available`, `GET .well-known/jwks.json`, and with Google `GET oauth/google`, `GET oauth/google/callback`, `GET oauth/providers`, `GET oauth/linked`. Bodies are goauth's (`{"data": {...}}`; login returns `data.access_token`, `data.refresh_token`). Their OpenAPI document is merged into `/openapi.json` under the base path; a component name used by both specs fails startup.
- Tokens: ES256 JWTs with `kid` (`AUTH_JWT_KEY_ID`), issuer, audience, `amr`, `auth_time`, `token_version`; public keys at `/v1/auth/.well-known/jwks.json`, so the delivery service can verify them without a shared secret. Refresh tokens rotate; reuse is rejected. Login also sets the `goauth_access_hookbeam` and `goauth_refresh_hookbeam` cookies (`config.SessionName`; Secure unless `AUTH_ALLOW_INSECURE_COOKIES`).
- `Auth.Authenticate(ctx, token)` validates online: signature, claims, the user's current `token_version`, suspension and session state. One DB read per request.
- Middleware `handlers/authn.go` runs in the generated router's `Middlewares`, reads each op's `security` from `openapi.GetSpec()` keyed by `r.Pattern` into an `accessRule{mode, user, apiKey}` (`bearerAuth`/`cookieAuth` → user session, `apiKeyAuth` → workspace API key; an unknown scheme name still requires a session), takes the token from `Authorization: Bearer` or the `config.AccessCookie` cookie, routes tokens starting with `hb_` to `APIKeyService.Authenticate` and everything else to goauth's `Authenticate` (through the `handlers.Authenticator` interface), and stores the result with `types.WithPrincipal`. A key on a session-only route, or a session on a key-only route, is 401.
- Modes: `security: []` anonymous; `bearerAuth`/`cookieAuth` required; a `- {}` alternative optional; route missing from the spec required; op omitting `security` inherits root, and with no root it is required (fail closed). A bad token is 401 even on optional routes.
- `GET /v1/me` (ours; sessions and keys) returns the principal: `userId` (null for a key), `workspaceId`, `authenticatedAt`, `expiresAt` (null for a key without expiry), `methods` (`password`, `google`, `api_key`). Profile fields are `GET /v1/auth/me`.
- Roles (issue 12): the spec's four roles are `owner`, `admin` (goauth's), `developer`, `viewer` (a `member_roles` row refines goauth's `member`; `developer` is the default). `services.Authorizer.Can(ctx, actor, Capability)` is the matrix (`CapReadMetadata` all, `CapReadPayloads`/`CapReplay` not viewer, `CapManageEndpoints`/`CapManageKeys`/`CapManageMembers` owner+admin, `CapManageOwners` owner) and every control-plane mutation calls it before the data plane; a refusal is 403 "a <role> cannot <capability>". `GET /v1/members`, `PUT /v1/members/{userId}/role {developer|viewer}` (sessions, manage-members). goauth's `POST .../invite`, `PUT .../members/{userId}`, `DELETE .../members/{userId}` are wrapped by `guardOrgRoute` → `services.OrgPolicy.Check`: nobody grants above their own goauth role, only owners touch owners, the workspace keeps at least one owner; the body is peeked and restored for goauth.
- Workspaces are goauth organizations (decision 0004). Signup auto-creates "<name>'s workspace"; `GET /v1/auth/org/my` lists them; `POST /v1/auth/org/switch {org_id}` returns a new token pair whose `active_org_id` becomes `Principal.OrganizationID`, after goauth checks membership and that the org is active. Members, invitations and roles (`owner`, `admin`, `member`, fixed in goauth) are goauth routes under `/v1/auth/org/{orgId}/...`. Hookbeam reads a role only through `services.Memberships.Role` (`app/workspaces.go` over goauth's storage), never from the token.
- `actor(r)` (handlers) is the helper for workspace routes: a user session with an active workspace → `services.Actor{UserID, WorkspaceID}`; an API key → 403 "a user session is required"; no workspace → 403 "select a workspace". `principal(r)` is for routes that accept keys too (`GetMessage`, `ListDeliveryAttempts`, `SubmitMessage`); they still require `OrganizationID`.
- Scopes are declared in the spec and enforced by the middleware: `apiKeyAuth: [messages:write]` on an operation means a key must hold every listed scope, else 403 "api key lacks scope …". `APIKeyService.Authenticate` returns the key's scopes next to the principal. `SubmitMessage` is `apiKeyAuth: [messages:write]` only (a session gets 401); `GetMessage` and `ListDeliveryAttempts` accept sessions or `apiKeyAuth: [messages:read]`; `GetMessagePayload` sessions or `apiKeyAuth: [payloads:read]`. Scope names are the spec's (`models.APIKeyScopes`: `messages:write` (default), `messages:read`, `payloads:read`, `deliveries:replay`). `ReplayDelivery` accepts sessions (replay capability) or `apiKeyAuth: [deliveries:replay]`; a key principal has no user, so `actorOrKey(r)` builds an actor with the workspace alone and `deliveryService.allow` skips the role matrix for it (the scope gate already ran).
- API keys (`api_keys`, Hookbeam's table, decision 0004): `POST/GET /v1/api-keys`, `DELETE /v1/api-keys/{id}`, session-only, owners and admins for create and revoke (`Authz.Can(CapManageKeys)`), members may list. The raw key is `hb_` + 43 url-safe base64 chars, returned once; only `prefix` (first 11 chars) and the SHA-256 `key_hash` are stored. `POST /v1/applications/{id}/api-keys` mints a key bound to one application (`api_keys.application_id`, migration 0005): `SubmitMessage` refuses it for any other application (403) before the data plane; `POST /v1/api-keys` stays workspace-wide. `Authenticate` returns the key itself (scopes, binding, id; hash cleared) and the middleware keeps it in the context (`apiKeyFrom(r)`). Submissions per key are counted in `Core.RateLimiter` under `api-key:<id>`, `LIMITS_KEY_SUBMISSIONS_PER_MINUTE` (600, per replica); over the limit is 429 with `Retry-After`. Scopes are a whitelist (`models.APIKeyScopes`: `messages:write` the default, `messages:read`). `Authenticate` loads by hash, rejects revoked or expired keys (401), touches `last_used_at` at most once a minute, and yields `Principal{OrganizationID, Methods: ["api_key"]}` with no user plus the scopes. Revocation is immediate and not reversible.
- Account emails (welcome, verification, reset) are sent by goauth's notification module through `mailer.NewGoAuthSender(core.Mailer)`, synchronously from its own event bus, not through the outbox. Links point at `AUTH_LINK_BASE_URL` + `/verify-email` or `/reset-password`.
- Rate limits are goauth's default rules counted in `Core.Cache` through `ratelimitmemory.Limiter.Take`: login and other credential checks 30/min per IP and 10/10min per identifier, sends 10/h per IP and 3/h per identifier, signup 10/h per IP, refresh 120/min per IP; 429 plus `Retry-After` and `Ratelimit-*` headers. Fixed windows are clock-aligned, so a burst straddling a boundary can pass twice. Each replica counts independently.
- Google: `oauth` module with PKCE and provider-verified email; the redirect URI to register is `AUTH_API_URL/v1/auth/oauth/google/callback`; after login the browser lands on `AUTH_LINK_BASE_URL/auth/callback`.
- **Rule:** a protected route is declared by `security` in the spec. Never check tokens in handlers; read `types.PrincipalFromContext(r.Context())` through the `principal(r)` helper, `!ok` → `apperr.NewUnauthorized` (copy `GetMe`).
- Not built: a check that a bound key's application exists (the contract has no GetApplication yet).

## OpenAPI workflow

1. Edit `api/openapi/paths/<resource>.yaml`: path items as top-level keys (`users:`, `user:`); request/response schemas and resource params in the same file's `components:` block, ref'd locally (`'#/components/schemas/User'`, `'#/components/parameters/UserId'`).
   - `components/*.yaml` is shared only: the `Error` schema and error responses. Never add resource schemas or parameters there.
   - Reuse another resource's schema via `'./users.yaml#/components/schemas/User'` (see `me.yaml`).
   - Component names must be unique across all files: the bundler names each by its last pointer segment, so duplicates collide.
2. New path file/route → add under `paths:` in `api/openapi/openapi.yaml`.
3. Every error response `$ref`s `components/responses.yaml` (`BadRequest, ValidationFailed, Unauthorized, NotFound, Conflict, Unavailable, InternalError`).
4. Public ops set `security: []`; protected ops list `bearerAuth` and `cookieAuth`. A public op must also be added to `publicOperations` in `handlers/authn_test.go` (ids as the embedded spec spells them, e.g. `CreateUser`); Every protected case in the resource's test table (`users_test.go`, `auth_test.go`, …) sets `auth: true`, which first sends the same request without a token and requires 401. Enforced by `handlers/authn.go`. An op that omits `security` requires login (fail closed), and `TestEveryOperationDeclaresSecurity` fails the build until it declares one.
5. `make gen-openapi` (optionally `make lint-openapi`; both take `APP=`) regenerates `api/gen/openapi`; build fails until `Handler` implements the new method.
- Non-strict server (`strict-server: false`): handler owns decode/write.
- `format: email` generates `string`; email and length validation live in services.

## List endpoints

- Params: inline on each list operation in its own file (copy `listUsers`): `limit` (default 20, 1..100), `offset`, `search`, `sort_by` enum (default `id`), `sort_dir` (`asc|desc`, default asc).
- Handler builds `repos.ListXOpts{Limit: defaultPageSize}` (20) and overrides from non-nil params; enums copy as `string(*params.SortBy)`.
- Repo exports `var XSortColumns = sorting.Columns{"id": "id", "createdAt": "created_at", ...}` (API key → column).
- Service validates `Limit` 1..100, `Offset >= 0`, and `maps.Copy(fields, repos.XSortColumns.Validate(opts.SortBy, opts.SortDir))` (422), calls `ListX` then `CountX`, returns `(items, total, err)`.
- Repo: `XForList` model without sensitive fields, `xListColumns` + shared `xListFilter` const used by both List and Count, `ORDER BY ` + `XSortColumns.OrderBy(opts.SortBy, opts.SortDir)` + ` LIMIT @limit OFFSET @offset`.
- `OrderBy`: empty/unknown key → `id`, non-`desc` → `ASC`, appends `, id <DIR>` tie-breaker. Default order `id ASC`.
- Response schema `XList` `{items, total}` in `paths/<resource>.yaml` `components:`; index sort columns that large tables sort by.

## Migrations

- Each app owns its migrations and database: `api/migrations/`, exposed as `migrations.Up(ctx, pool)`.
- Copy the latest `api/migrations/NNNN_*.go` to the next number, e.g. `0003_add_foo.go` (4-digit prefix is load-bearing), rename the methods; add `s.registerMigration_NNNN_AddFoo()` to `registerMigrations()` in `api/migrations/migration.go`.
- Implement both `up_` and `down_`; check `make migrate-up && make migrate-down && make migrate-up` (add `APP=<name>` for another app).
- New table: `id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`; FK columns `BIGINT`.
- New table with `updated_at`: `CREATE TRIGGER update_<table>_updated_at BEFORE UPDATE ON <table> FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();` (function from 0001).
- Index on existing table: `goose.AddMigrationNoTxContext` with `*sql.DB` and `CREATE INDEX CONCURRENTLY IF NOT EXISTS` (template in the `add-migration` skill). Indexes on a table created in the same migration are plain `CREATE INDEX`.
- Expand/contract: add → deploy code → backfill → drop. Never rename or drop a column the running code reads.
- Update `models` `db` tags and repo column consts in the same change.
- Never run migrations at boot in prod; deploy runs `api -m migrate up` as a one-off task.

## Testing

| Target | Pattern |
|---|---|
| services | `services_test` package, testify `suite`, `repo_mocks.NewMockXRepo(gomock.NewController(s.T()))`, assert `*apperr.AppError.Code`; table of cases where a method has several inputs |
| repos | `//go:build integration`, `TestMain` → `os.Exit(testutil.Main(m))` once per package, suite with `testutil.Postgres(s.T(), migrations.Up)` (that service's `migrations` package) in `SetupSuite`, `testutil.Truncate` in `SetupSubTest`, a `seed(...)` helper for fixtures; one table-driven method per repo method (example: `repos/outbox_test.go`); no `t.Parallel`. Delivery repos assert the workspace scope (a row of another workspace is `ErrNotFound`) |
| rpc (delivery) | `rpc/server_test.go`: real `rpc.Server` with `grpcx.ServerInterceptors` over `bufconn`, `service_mocks`, table per RPC asserting `status.Code`; `scoped(t)` attaches the workspace metadata, a case without it expects `Unauthenticated` |
| delivery services | `messages_test.go` (accept, duplicate, conflict, concurrent loser, no subscriber, validation, cancel, replay), `dispatch_test.go` (a `fakeSender`; success, retry, exhaustion, archived endpoint, lost lease, load failure releases, two secrets both sign; `TestSweep`: unknown attempt recorded and rescheduled or exhausted, load failure, lease taken meanwhile; `TestCrashBetweenSendAndRecord`: send, crash, sweep, resend = one duplicate with the same `webhook-id`; the recorded headers verify with `pkg/webhooks`), `policy_test.go` (every row of the spec's table with jitter off), `sender/http_test.go` (httptest server: statuses, redirect recorded as 3xx, timeout, connection, dns, blocked destination, tls) |
| api services (delivery) | `delivery_test.go`: a `fakeClient` implementing the generated client records the outgoing metadata and answers from function fields; asserts scope metadata, byte-for-byte payload forwarding, `FromStatus` mapping |
| jobs | `delivery_state_test.go` (decode and forward to `EndpointHealthService`, malformed payload errors); plain unit tests over one `newJobs(t)` fixture (`service_mocks.MockEventService` plus a counting handler registered in `Handlers`): `jobs_test.go` covers `New`, `Handle` (new, replay, handler error, unknown type, invalid envelope, timeout) with the mock `ProcessOnce` calling `fn` via `DoAndReturn`; a real handler gets its own `<name>_test.go` over service mocks and `mailermemory` |
| services (events) | `events_test.go`: unit with `repo_mocks.MockOutboxRepo` + `memory.Bus`; `events_integration_test.go` (`//go:build integration`, own `TestMain`): 4 concurrent `Publish` callers over a real outbox, every event published exactly once; `ProcessOnce` replay and failed-fn cases over a real `processed_events` |
| pkg/events/sqs | `sqs_integration_test.go` (`//go:build integration`) publishes through `sns` and consumes through `sqs`; skipped unless `EVENTS_AWS_ENDPOINT_URL` is set (LocalStack from `make queue-up`) |
| handlers | `members_test.go` (members routes; `TestOrgRouteGuard` mounts fake goauth routes and asserts the policy runs before them); tests mirror source files: `handler_test.go` holds only the fixture + `endpointCase`/`runEndpoints`; `health_test.go`, `me_test.go`, `api_keys_test.go`, `applications_test.go`, `endpoints_test.go`, `messages_test.go` (`submit` helper sends a key with given scopes), `deliveries_test.go`, `router_test.go` (goauth routes mounted, spec merged, colliding specs rejected), `authn_test.go` (pins `publicOperations` and `apiKeyOperations`; session, cookie, key, wrong-credential-kind and scope cases) hold that file's cases. Parameter binding runs before authentication, so a missing required header is 400 with no credential check; the fixture's `f.delivery` is a `MockDeliveryService`. One `newFixture(t)` (or `newFixtureWithRouter(t, RouterOptions)`) with `service_mocks.NewMockXService` + `handlers.New` + `handlers.NewRouter`; its `fakeAuth` accepts token `"good"` as the `ada` principal (user `u_ada` in workspace `ws_1`; `f.auth.principal` is editable per case) and `f.apiKeys` is a `MockAPIKeyService` (tokens starting with `hb_` reach it); `f.do(method, path, body)` sends `Authorization: Bearer good`, `f.send(req)` sends a custom request (no token, cookie `accessCookie`); `TestAuthentication` covers missing, invalid, bearer, cookie, public; table test through the router asserting status and `httpx.ErrorResponse.Error`; each case carries `auth bool` (protected → probed without a token first, expects 401; binding-error cases stay `false`). goauth's own routes are not tested here |
| pkg | plain unit tests (`pkg/httpx`, `pkg/apperr`, `pkg/config`, `pkg/logger` incl. the goauth adapter, `pkg/mailer` incl. `GoAuthSender`, `pkg/ratelimit/memory` incl. `Take`, `pkg/sorting`, `pkg/secrets`, `pkg/certs`, `pkg/grpcx`: real loopback listener with certificates from `pkg/certs` for the mTLS cases, a hand-registered one-method service for the interceptor cases) |

Shape every test the same way: one test per function or repo method, `tests := []struct{ name; inputs; want; wantErr error }`, `s.Run(tt.name, ...)` per case, then `seed → call → if tt.wantErr != nil { ErrorIs; return } → assert`. Fixtures come from a seeder helper, never copy-pasted setup. Not-found cases use `int64(-1)`; created ids are asserted with `s.Positive(x.ID)`; handler paths use `strconv.FormatInt(id, 10)`.

Mocks: put `//go:generate mockgen -source=<file>.go -destination=<name>_mocks/mock_<file>.go -package=<name>_mocks` under the package clause of each interface file; `make gen-mocks`; commit `mock_*.go`. Mocks live in a `<name>_mocks` subpackage (`repos/repo_mocks`, `services/service_mocks`), never in the production package.

## Style

- Comments minimal: one line per exported identifier, short package doc; no README or decision refs in new code.
- No speculative code, options, or interfaces nothing uses.
- gofmt + goimports with local prefix `github.com/bete7512/hookbeam` (`make fmt`).
- Makefile targets, hyphenated; no Taskfile.

## Definition of done

1. `make gen-openapi` if the spec changed; `make gen-proto` if the contract changed.
2. `go generate ./api/...` for touched interfaces (or `make gen-mocks`).
3. `make fmt`
4. `make lint`
5. `make test`
6. `make test-integration` if repos or migrations changed.
7. `make gen-check` (regenerates and fails if any generated file changed).
8. New endpoint: live curl check against `make dev` (see `verify-change` skill).

## Do not

- No ORM, sqlc, or SQL builders; no strict server.
- No SQL outside `<service>/repos`: not in `pkg/`, `jobs/`, `services/`, `handlers/` or `rpc/`. A new table gets a repo, even an infrastructure table.
- No gRPC types in `services/` or `repos/`; `rpc/` maps proto to models. No `status.Error` by hand in `rpc/`; return the service's `apperr` error through `grpcx.Status`.
- No plaintext gRPC between the planes outside local development; no reading the workspace from a request field, it comes from metadata through the interceptor.
- No publishing to the queue from services or handlers; write an outbox row and let the relay publish.
- No per-request counters, sessions-like hot state or caches in Postgres; use `Core.Cache` (`pkg/cache`), never a second cache instance or a package-level map.
- No identity tables, password handling, token issuing or account emails in this repo; goauth owns them. No second auth library, no SQL against goauth's tables.
- No `git add`, `git commit` or `git push` unless the user explicitly asks for it.
- No client input in `ORDER BY` or any SQL string; sort keys go through `sorting.Columns`, values through `pgx.NamedArgs`.
- No UUID primary keys for Hookbeam tables; ids are bigint identity / `int64`. goauth's VARCHAR(36) ids are referenced as `string` columns and fields (decision 0003).
- No routes outside the spec, except goauth's under `/v1/auth` mounted from `Auth.Routes()` (plus `/openapi.json`, `/docs` and the `/` 404 in `router.go`).
- No logging in services or repos; no global loggers/pools/config.
- No `panic` in request paths.
- No hand edits under `<service>/gen/`.
- No imports across services (`api` → `delivery/...`), no shared tables between services; the control plane reaches the data plane only through `contracts/gen/go`.
- No migrations at app boot.
- No `x-` custom spec extensions for auth; auth reads `security`.
- No token parsing or `Authorization` header checks in handlers or services; the middleware owns it, handlers read `types.PrincipalFromContext`.
- No Taskfile.

## Skills

| Skill | Use when |
|---|---|
| `add-endpoint` | adding a single-resource operation (spec → handler → service → repo → tests) |
| `add-list-endpoint` | adding a paginated, searchable, sortable list (`ListXOpts`, `XForList`, `XSortColumns`, `{items,total}`) |
| `add-migration` | any schema change |
| `add-job` | a new event type: outbox write in the producing service + worker handler |
| `verify-change` | before declaring done: gates + live curl check |

## Planned, not built (do not invent)

- Auth, remaining: goauth's `audit` module once it no longer requires `admin`; a second OIDC provider is one more `Providers` entry.
- Authz (Day 16): `Authorizer` interface, RBAC adapter + OpenFGA adapter.
- Events, remaining: NATS or other transport adapters, payload versioning, worker autoscaling on queue depth (Day 14).
- Observability: OTel, metrics, Sentry/GlitchTip, dashboards, runbooks.
- Delivery, remaining (milestone 3, GitHub issues): per-endpoint attempt rate, circuit breaking; endpoint health episodes and notifications over the projection (milestone 4); list endpoints for messages; duplicate-key detection inside JSON objects; ids as decimal strings for JavaScript clients if the console needs it (decision 0008 says int64).
- Infra: Terraform, Ansible, ECS deploy workflow. Built: one Dockerfile per binary next to its `main.go` (`api/cmd/{api,worker}/Dockerfile`, `delivery/cmd/{server,worker}/Dockerfile`, distroless, non-root, built from the repo root) and `.github/workflows/ci.yml` (lint, gen-check incl. buf, unit + integration against a Postgres service and LocalStack, govulncheck, four image builds + Trivy).
