# hookbeam — AI working guide

## What this is

Hookbeam, an open-source webhook delivery platform (`product/README.md` is the spec). Go 1.26 + Postgres 16, module `github.com/bete7512/hookbeam`, one Go module.
Spec-first OpenAPI (oapi-codegen, std `net/http` ServeMux), hand-written pgx repos, goose Go migrations.
Services live at the repository root. `api/` is the control plane (identity, workspaces, API keys, public REST; today it has users CRUD, authentication with email verification, password reset, rate limits and Google login, health, transactional email): two binaries, `api/cmd/api` (HTTP) and `api/cmd/worker` (outbox relay, outbox cleanup, event handlers over SNS/SQS), share its packages (`app`, `config`, `services`, `repos`, `models`, `events`, `jobs`). `delivery/` (data plane: delivery service and worker) and `console/` (Next.js) are planned, not built. Makefile is the only entrypoint.

## Commands

`make` loads `.env` if present (else defaults matching `.env.example`). Host Postgres port defaults to **5433**.
The api binary runs migrations itself: `api -m migrate <goose cmd>` (`up`, `down`, `status`, `up-to N`…).
`APP ?= api` (and `SVC := $(APP)`) selects the service for per-service targets: `make run-api`, `make migrate-up APP=delivery`. `SERVICES` lists the folders `gen-check` hashes.

| Target | Does |
|---|---|
| `setup` | install oapi-codegen, mockgen, air, golangci-lint, govulncheck; create `.env` |
| `db-up` / `db-down` / `db-reset` | compose `core` profile Postgres: start+wait / stop / wipe volume |
| `migrate-up` / `migrate-down` / `migrate-status` | `go run ./$(APP)/cmd/api -m migrate up\|down\|status` |
| `run-api` / `dev` | `go run ./$(APP)/cmd/api` on `:8080` / `db-up` then `run-api` |
| `run-worker` | `go run ./$(APP)/cmd/worker`: relay + cleanup + subscriber until SIGTERM |
| `queue-up` / `queue-down` / `queue-logs` | compose `queue` profile: LocalStack (SNS+SQS, `:4566`, `deploy/localstack/init.sh` creates topic, queue, DLQ) and Mailpit (`:1025` SMTP, `:8025` UI) |
| `build` | `bin/$(APP)/api`, `bin/$(APP)/worker` (now `bin/api/api`, `bin/api/worker`) |
| `gen` | `gen-openapi` + `gen-proto` (stub) + `gen-mocks` |
| `gen-openapi` | bundle `$(APP)/openapi` → `$(APP)/gen/openapi/openapi.bundle.yaml`, then oapi-codegen |
| `gen-mocks` | `go generate ./...` |
| `gen-check` | hashes generated files under the service folders, runs `make gen`, fails if anything changed (no git needed) |
| `lint` / `lint-openapi` / `fmt` / `vet` | golangci-lint / kin-openapi validate (`APP`) / gofmt+goimports / go vet |
| `test` | `go test -race ./...` (no Docker) |
| `test-integration` | same with `-tags integration`; testcontainers, or with `TEST_DATABASE_URL` each package creates and drops its own `test_<hash>` database on that server |
| `test-cover` / `tidy` / `vuln` | coverage html / go mod tidy / govulncheck |
| `image` / `images` | `docker build -f $(APP)/cmd/$(TARGET)/Dockerfile .` (`make image TARGET=worker`) / both binaries; tags `hookbeam-<app>-<target>:<git sha>` |

## Layout

A service = a business area that owns its data; its processes (api, worker) live inside it.

```
api/                        control plane; delivery/ (data plane, planned) copies this shape
  cmd/api/main.go                    HTTP process: config → app.NewCore → handlers → server (or -m migrate)
  cmd/<target>/Dockerfile            image for that binary; build context is the repo root
  cmd/worker/main.go                 background process: config → app.NewCore → errgroup of publishEvents (Events.Publish every OUTBOX_POLL_INTERVAL), cleanOutbox (Events.CleanOutbox hourly), Subscriber.Run(jobs.Handle) until SIGTERM
  app/core.go                        composition root shared by this app's binaries: pool, Tx, repos, hasher, tokens, services, publisher, subscriber, mailer
  config/                            app config: embeds pkg/config.Config, adds Auth (AUTH_*), Events (EVENTS_*), Outbox (OUTBOX_*), Mailer (MAILER_*), RateLimit (RATELIMIT_*), OIDC (OIDC_GOOGLE_*)
  handlers/                          HTTP only; imported only by cmd/api: authn.go (spec-driven auth), ratelimit.go (per-IP middleware), oidc.go (Google redirect + callback)
  {services,repos,models}/           shared by api and worker (+ repos/repo_mocks, services/service_mocks); repos/outbox.go, processed_events.go, auth_tokens.go, user_identities.go own the plumbing tables
  events/                            event type constants + payload structs (UserRegistered, UserVerificationRequested, PasswordResetRequested)
  jobs/                              worker only: jobs.New(Deps) → Jobs with handler methods (welcome.go), a type→handler map, and Handle (EventService.ProcessOnce per event); welcome.go, verification.go, password_reset.go
  migrations/                        goose Go migrations NNNN_name.go + migration.go registry; migrations.Up(ctx, pool); 0003 outbox, 0004 processed_events, 0005 auth_tokens, 0006 user_identities
  openapi/openapi.yaml               root: info, tags, security, paths: $ref ./paths/<file>.yaml#/<key>
  openapi/paths/<resource>.yaml      path items + that resource's schemas/params in a local components: block
  openapi/components/*.yaml          shared only: Error, error responses
  gen/openapi/                       generated: openapi.gen.go, openapi.bundle.yaml (committed, never edit)
pkg/apperr            sentinels + AppError
pkg/auth              PasswordHasher (argon2id), Tokens (EdDSA JWT), NewToken/HashToken, WithClaims/ClaimsFrom
pkg/config            common env → Config (App, HTTP, Log, DB), validated at boot
pkg/db                pool (+retries), DBTX, WithTx, Transactor/NewTransactor, HandleError
pkg/events            Event envelope, Publisher, Subscriber, Handler; adapters memory/ (Bus), sns/ (NewClient, Publisher) and sqs/ (NewClient, Subscriber, WithLogger, WithVisibilityTimeout)
pkg/httpx             server, middleware, WriteJSON/WriteError/DecodeJSON, /docs, 404
pkg/logger            slog builder, request-id/context helpers
pkg/mailer            Message, Mailer; adapters memory/, smtp/ (Mailpit locally), ses/
pkg/oidc              OpenID Connect code flow with PKCE: Provider{AuthCodeURL, Exchange}, Identity, New(ctx, Config), NewState, NewVerifier
pkg/cache             Cache over ristretto: New(Config{MaxItems}), Get, Set(key, value, ttl), Wait, Close; one instance per app, built in Core
pkg/ratelimit         Limiter{Allow(ctx, key, limit, window) Decision}, Window, Decide; memory/ adapter counting in a cache.Cache
pkg/sorting           Columns: sort-key whitelist → OrderBy / Validate
pkg/testutil          Postgres(t, migrate) (integration tag; nil migrate = no migrations), Truncate, Main, Ptr, MustEnv
scripts/openapi-bundle/  shared tool, -in/-out per app
deploy/localstack/init.sh  creates topic hookbeam-events, queue hookbeam-worker (raw delivery), DLQ hookbeam-worker-dlq (maxReceiveCount 3)
docs/adr/             decisions (0001–0007, 0016–0018)
go.mod                one module; per-service go.mod + go.work when delivery/ arrives
```

Import paths: `github.com/bete7512/hookbeam/api/<pkg>`, `.../api/migrations`, `.../api/gen/openapi`.

Planned, absent: `delivery/`, `console/`, `infra/`, ECS task definitions, proto contracts.

## Services and split-readiness

1. A service imports only its own packages + `pkg/`. Cross-service imports are a convention, not compiler-enforced (no `internal/`); `pkg/` importing a service is blocked by the `depguard` rule in `.golangci.yml` (deny list: `api`, `delivery`, `console`).
2. Each service owns its migrations, database URL, OpenAPI spec, generated code and config.
3. Services talk only through contracts (gRPC client or events); never shared tables or Go structs.
4. `pkg/` stays domain-free: `testutil.Postgres(t, migrate)` takes the service's migrate func; `pkg/config` holds only common settings, service settings live in `<service>/config`.
5. New service = copy the `api/` folder shape to a new root folder, add it to `SERVICES` in the Makefile and to the depguard deny list. The planned second service is `delivery/`; a new business area with its own data is never a folder inside an existing service.

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
| `handlers` | implement `openapi.ServerInterface` with raw `(w, r, params)`; DTO⇄model mapping (`toUserDTO`) | `api/gen/openapi`, `models`, `services` (interfaces), `pkg/httpx`, `pkg/apperr`, `pkg/auth` (`ClaimsFrom`), `pkg/oidc`, `pkg/ratelimit`; `repos` **only** for `ListXOpts` | SQL, pgx, status mapping, logging, token parsing |
| `services` | validation (`apperr.NewValidation`), business rules, repo-error classification; `EventService` publishes and prunes the outbox | `models`, `repos` (consume `repos.XRepo` directly), `pkg/apperr`, `pkg/auth`, `pkg/events` (interfaces) | HTTP, DTOs, SQL, logging (keep `Logger` in Deps, unused), loops |
| `repos` | hand-written SQL, `XSortColumns` whitelist; the only place SQL lives (also `outbox`, `processed_events`) | `models`, pgx, `pkg/db`, `pkg/apperr`, `pkg/sorting` | business rules, HTTP, logging |
| `models` | structs with `db:"col"` tags (also row types) | stdlib | everything else |
| `events` | event type constants + payload structs | stdlib | everything else |
| `jobs` | `Jobs`: handler methods over `Deps`, listed in the map in `New`, dispatched by `Handle`; may log | `services` (interfaces), `pkg/events`, `pkg/mailer`, `events` | SQL, `repos`, pgx, HTTP, `handlers` |
| `app` | `Core`: pool, `Tx`, repos, hasher, `auth.Tokens`, services, publisher, subscriber, mailer | `pkg/*`, `repos`, `services` | `handlers` (api-only) |

- `api/handlers` is imported only by `api/cmd/api`; `api/app` never imports handlers.

Ids: `BIGINT GENERATED ALWAYS AS IDENTITY` in SQL, `int64` in Go at every layer, `type: integer, format: int64, minimum: 1` in the spec (ADR 0018).

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
- `api/app/core.go` is the app's shared composition root: `app.NewCore(ctx, app.Deps{Config, Logger})` → `*app.Core{Pool, Cache, Users, Auth, Events, RateLimiter, Publisher, Subscriber, Mailer, Google}` (services, ports and the shared cache; repos stay inside `NewCore`); `Close()` releases the cache and the pool.
- `NewCore` builds `userRepo`, `sessionRepo`, `outboxRepo`, `processedRepo`, `authTokenRepo`, `identityRepo`, the app cache (`cache.New(CACHE_MAX_ITEMS)`) and `ratelimitmemory.New(cache)`, the Google `oidc.Provider` when configured, `tx := db.NewTransactor(pool)`, the argon2id hasher, `auth.NewTokens(seed, issuer, ttl)`, the events adapters (`EVENTS_BACKEND`: `memory` bus or `aws` SNS/SQS), the mailer (`MAILER_BACKEND`: `memory`, `smtp`, `ses`), then `UserService` (Users, Sessions, Hasher, Tx, Outbox), `AuthService` (Users, Sessions, AuthTokens, Identities, Outbox, Tx, Hasher, Tokens, Limiter, LoginLimit/Window, RefreshTokenTTL, ActionTokenTTL) and `EventService` (Tx, Outbox, Processed, Publisher, BatchSize, Retention).
- Config: `api/config` embeds `pkg/config.Config` (App, HTTP, Log, DB) and adds `Auth` (`AUTH_ARGON2_*`, `AUTH_JWT_SIGNING_KEY`, `AUTH_ISSUER`, `AUTH_*_TOKEN_TTL`), `Events` (`EVENTS_BACKEND`, `EVENTS_AWS_REGION`, `EVENTS_AWS_ENDPOINT_URL`, `EVENTS_TOPIC_ARN`, `EVENTS_QUEUE_URL`; AWS creds from `AWS_*`), `Outbox` (`OUTBOX_POLL_INTERVAL` 500ms, `OUTBOX_BATCH_SIZE` 100, `OUTBOX_RETENTION` 168h), `Mailer` (`MAILER_BACKEND`, `MAILER_SMTP_ADDR`, `MAILER_FROM`), `Auth.ActionTokenTTL`/`LinkBaseURL` (`AUTH_ACTION_TOKEN_TTL` 30m, `AUTH_LINK_BASE_URL`), `RateLimit` (`RATELIMIT_AUTH_LIMIT` 5 per `RATELIMIT_AUTH_WINDOW` 1m per IP; `RATELIMIT_LOGIN_LIMIT` 5 per `RATELIMIT_LOGIN_WINDOW` 15m per account), `Cache` (`CACHE_MAX_ITEMS` 100000), `OIDC` (`OIDC_GOOGLE_CLIENT_ID`, `_SECRET`, `_REDIRECT_URL`; empty id = routes answer 503). `pkg/config.HTTP.TrustProxy` (`HTTP_TRUST_PROXY`) decides whether `X-Forwarded-For` is believed.
- `api/cmd/api/main.go`: config → logger → `app.NewCore` → `handlers.New(handlers.Deps{Users, Auth, DB, Limiter: core.RateLimiter, RateLimit{Limit, Window, TrustProxy}, Google: core.Google, Logger})` → router → server.
- `api/cmd/worker/main.go`: config → logger → `app.NewCore` → `j := jobs.New(jobs.Deps{Events, Users, Auth, Mail, LinkBaseURL: cfg.Auth.LinkBaseURL, Logger})` → errgroup of `publishEvents` (calls `Events.Publish` now and every `OUTBOX_POLL_INTERVAL`), `cleanOutbox` (calls `Events.CleanOutbox` now and hourly) and `core.Subscriber.Run(gctx, j.Handle)` → SIGTERM cancels. The two loops are plain ticker functions in `main.go` that log a warning on failure; services never loop or log.

Wiring a new resource `Project`:
1. `api/app/core.go`: add `Projects services.ProjectService` to `Core`.
2. `core.go`: `projectRepo, err := repos.NewProjectRepo(pool)`, then `services.NewProjectService(services.ProjectServiceDeps{Projects: projectRepo, Logger: d.Logger})` → `Core.Projects`.
3. `api/handlers/handler.go`: add `Projects services.ProjectService` to `Deps` and to the nil check in `New`.
4. `api/cmd/api/main.go`: pass `Projects: core.Projects` in `handlers.Deps{...}`; update `newFixture` in `handler_test.go`; add `projects_test.go` with `runEndpoints(t, []endpointCase{...})`.
5. Worker needs it: read `core.Projects` in `api/cmd/worker/main.go`.

## Events and jobs

- Producing: inside the service's `Tx.WithTx` block, `deps.Outbox.WithTx(tx).CreateOutboxEvent(ctx, &models.OutboxEvent{Type: apievents.X, Payload: json})`. Same transaction as the row that caused it; never publish from a service or handler.
- Publishing (`services.EventService.Publish`): drains the outbox batch by batch, one transaction each: `ClaimOutboxEvents(limit)` (`FOR UPDATE SKIP LOCKED`) → `Publisher.Publish` → `MarkOutboxEventsPublished`; stops after a short batch and returns the total. A failed batch rolls back and the error is returned; the worker retries next tick.
- Pruning (`services.EventService.CleanOutbox`): `DeleteOutboxEventsPublishedBefore(now - OUTBOX_RETENTION, batch)` until a short batch; the worker calls it hourly.
- Consuming: `Subscriber.Run(ctx, j.Handle)`; `Jobs.Handle` looks up the handler by type and calls `EventService.ProcessOnce(ctx, e.ID, fn)`, which in one transaction runs `ProcessedEventRepo.MarkEventProcessed(id)` (`ON CONFLICT DO NOTHING`; `false` = replay, `fn` skipped) and then `fn`, the registered `jobs.Handler(ctx, event)`. Handler error → rollback of the mark → the queue redelivers (SQS: 3 receives, then the DLQ). Unknown type or invalid envelope → error, retained for configured DLQ redrive. Jobs have bounded contexts and SQS renews visibility while processing one message per sequential consumer.
- Handlers call services and ports (`services.UserService`, `mailer.Mailer`), never repos. Their own writes run outside the mark's transaction, so a handler is retried as a whole on failure and must tolerate that.
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
| `NewInternal(err)` | 500 | `INTERNAL_ERROR` (message is generic) |
| `httpx.DecodeJSON` body over limit | 413 | `PAYLOAD_TOO_LARGE` |

Sentinels: `ErrNotFound, ErrConflict, ErrValidation, ErrBadRequest, ErrUnauthorized, ErrForbidden, ErrUnavailable, ErrRateLimited, ErrInternal`.

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

- `pkg/auth.Tokens`: Ed25519 (EdDSA) JWT access tokens with `sub`, `tv` (token_version), `jti`, `iat`, `exp`, `iss`; `Parse` accepts only EdDSA, errors wrap `auth.ErrInvalidToken`.
- Refresh tokens are opaque random strings (`auth.NewToken`); only their SHA-256 (`auth.HashToken`) is stored.
- Table (`migrations/0002_create_sessions.go`): `sessions`, one row per login with `refresh_token_hash`, `previous_token_hash`, `expires_at`, `revoked_at`; BIGINT identity id.
- `repos.SessionRepo`: `CreateSession`, `FindSessionByToken` (matches current or previous hash), `RotateRefreshToken` (one UPDATE that moves current to previous; stale or revoked → `ErrNotFound`), `RevokeSession`, `RevokeUserSessions`.
- Refresh with a token matching `previous_token_hash` counts as reuse and revokes the session. Tokens older than one rotation are unknown: rejected with 401, session untouched.
- `services.AuthService.Login`: one 401 message for unknown email, wrong password or no password; on an unknown email it still hashes the password (result discarded) so timing matches.
- `Refresh` first checks the session credential generation against the user and then rotates; reuse or a lost concurrent rotation revokes the session → 401; expired token or revoked session → 401. `Logout` is idempotent.
- `LogoutAll` and `UserService.ChangePassword` bump `users.token_version` and revoke all sessions.
- `Authenticate` parses the token and compares `tv` with the user's `token_version` (one DB read per request).
- Middleware `handlers/authn.go` runs in the generated router's `Middlewares`, reads each op's `security` from `openapi.GetSpec()`, keyed by `r.Pattern`.
- Modes: `security: []` anonymous; `bearerAuth`/`cookieAuth` required; a `- {}` alternative optional; route missing from the spec required; op omitting `security` inherits root, and with no root it is required (fail closed). A bad token is 401 even on optional routes.
- Token source: `Authorization: Bearer` or the `access_token` cookie. Claims go into ctx via `auth.WithClaims`.
- Routes: public `POST /v1/auth/login|refresh|logout|verify-email|resend-verification|forgot-password|reset-password`, `GET /v1/auth/google`, `GET /v1/auth/google/callback`; authenticated `POST /v1/auth/logout-all`, `GET /v1/me`. Token body `{accessToken, refreshToken, tokenType: "Bearer", expiresIn}`.
- Action tokens (`auth_tokens`, ADR 0006): `AuthService.IssueActionToken(userID, purpose, requestID)` derives the raw token from independent `AUTH_ACTION_TOKEN_KEY`, atomically advances the unique user/purpose slot for newer outbox request IDs, and preserves token/expiry on retries; the raw token goes only into the email link (`AUTH_LINK_BASE_URL/verify-email?token=` or `/reset-password?token=`). `repos.AuthTokenRepo.ConsumeAuthToken(hash, purpose)` is one UPDATE that succeeds once. Purposes: `models.TokenPurposeEmailVerification`, `TokenPurposePasswordReset`.
- Verification: register → `user.registered` → worker `Welcome` issues the token and mails the link (skipped when already verified); `POST /v1/auth/verify-email {token}` → `VerifyEmail` → `UserRepo.MarkEmailVerified`. `POST /v1/auth/resend-verification {email}` and `POST /v1/auth/forgot-password {email}` always answer 202 and only write an outbox event (`user.verification_requested`, `password.reset_requested`) when the account exists, so nothing reveals registered emails. The worker issues the tokens, never the api, so raw tokens never enter the outbox.
- Reset: `POST /v1/auth/reset-password {token, newPassword}` → `ResetPassword`: validate, consume, `UpdateUserPassword` (bumps `token_version`), `RevokeUserSessions`, all in one transaction → 204.
- Rate limits: middleware gives separate IP budgets to login, OIDC, registration, recovery, action-token use and sessions. Login also limits normalized account keys; recovery requests coalesce per recipient/purpose. Shared Core.Cache owns atomic bounded counters independent of evictable cached values. Active counters cannot evict. Over-limit returns 429 plus Retry-After; storage saturation returns 503. Each replica counts independently and restart resets counters. Shared quotas and general trusted-proxy chain validation remain architectural debt.

- Google login (ADR 0007): `GET /v1/auth/google` sets the `oidc_flow` cookie (`state.verifier`, HttpOnly, Secure, Lax, 10 min, path `/v1/auth/google`) and redirects; the callback checks `state` against the cookie, calls `oidc.Provider.Exchange`, then `AuthService.LoginWithIdentity(provider, identity)`: identity row exists → session; else Google must be authoritative for the verified address (Gmail or signed hd); non-authoritative addresses fail closed; account with that email exists and is verified → link; exists but unverified → link, `ClearUserPassword`, revoke sessions, mark verified (pre-hijacking defence); else create a verified account without a password. Provider `email_verified=false` → 401. The API constructs Google with bounded discovery; the worker does not discover OIDC. The handler provider is nil (routes 503) until `OIDC_GOOGLE_CLIENT_ID` is set.
- Config: independent `AUTH_ACTION_TOKEN_KEY` and `AUTH_JWT_SIGNING_KEY` required, base64 of 32 bytes (`openssl rand -base64 32`); `AUTH_ISSUER` (hookbeam), `AUTH_ACCESS_TOKEN_TTL` (15m), `AUTH_REFRESH_TOKEN_TTL` (720h), `AUTH_ACTION_TOKEN_TTL` (30m), `AUTH_LINK_BASE_URL`.
- **Rule:** a protected route is declared by `security` in the spec. Never check tokens in handlers; read `auth.ClaimsFrom(r.Context())`, `!ok` → `apperr.NewUnauthorized` (copy `GetMe`).
- User service endpoints enforce self-only ownership, including list/count scoping. Trusted internal jobs may use GetUser; HTTP uses GetUserFor. Organization roles remain planned.

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
| services | `services_test` package, testify `suite`, `repo_mocks.NewMockXRepo(gomock.NewController(s.T()))`, argon2 low params (`Memory: 8*1024, Iterations: 1`), assert `*apperr.AppError.Code`; table of cases where a method has several inputs |
| repos | `//go:build integration`, `TestMain` → `os.Exit(testutil.Main(m))` once per package, suite with `testutil.Postgres(s.T(), migrations.Up)` (`github.com/bete7512/hookbeam/api/migrations`) in `SetupSuite`, `testutil.Truncate` in `SetupSubTest`, a `seed(...)` helper for fixtures; one table-driven method per repo method (example: `repos/users_test.go`); no `t.Parallel` |
| jobs | plain unit tests over one `newJobs(t)` fixture (`service_mocks.MockEventService`, `MockUserService`, `mailermemory`): `jobs_test.go` covers `Handle` (new, replay, handler error, unknown type) with the mock `ProcessOnce` calling `fn` via `DoAndReturn`; `welcome_test.go`, `verification_test.go`, `password_reset_test.go` cover the mail handlers (token issued through `MockAuthService`, link asserted against `linkBaseURL`) |
| services (events) | `events_test.go`: unit with `repo_mocks.MockOutboxRepo` + `memory.Bus`; `events_integration_test.go` (`//go:build integration`, own `TestMain`): 4 concurrent `Publish` callers over a real outbox, every event published exactly once; `ProcessOnce` replay and failed-fn cases over a real `processed_events` |
| pkg/events/sqs | `sqs_integration_test.go` (`//go:build integration`) publishes through `sns` and consumes through `sqs`; skipped unless `EVENTS_AWS_ENDPOINT_URL` is set (LocalStack from `make queue-up`) |
| handlers | tests mirror source files: `handler_test.go` holds only the fixture + `endpointCase`/`runEndpoints`; `users_test.go`, `auth_test.go`, `health_test.go`, `router_test.go`, `authn_test.go`, `ratelimit_test.go` (per-IP table with `newFixtureWithRateLimit`), `oidc_test.go` (`fakeProvider` records state/verifier; login, callback and disabled cases) hold that file's cases. One `newFixture(t)` with `service_mocks.NewMockXService` + `handlers.New` + `handlers.NewRouter`; its `auth` mock accepts token `"good"` (→ `ada.ID`); `f.do(method, path, body)` sends `Authorization: Bearer good`, `f.send(req)` sends a custom request (no token, cookie); `TestAuthentication` covers missing, invalid, cookie, public; table test through the router asserting status and `httpx.ErrorResponse.Error`; each case carries `auth bool` (protected → probed without a token first, expects 401; binding-error cases stay `false`) |
| pkg | plain unit tests (`pkg/httpx`, `pkg/apperr`, `pkg/config`, `pkg/logger`, `pkg/auth`, `pkg/sorting`) |

Shape every test the same way: one test per function or repo method, `tests := []struct{ name; inputs; want; wantErr error }`, `s.Run(tt.name, ...)` per case, then `seed → call → if tt.wantErr != nil { ErrorIs; return } → assert`. Fixtures come from a seeder helper, never copy-pasted setup. Not-found cases use `int64(-1)`; created ids are asserted with `s.Positive(x.ID)`; handler paths use `strconv.FormatInt(id, 10)`.

Mocks: put `//go:generate mockgen -source=<file>.go -destination=<name>_mocks/mock_<file>.go -package=<name>_mocks` under the package clause of each interface file; `make gen-mocks`; commit `mock_*.go`. Mocks live in a `<name>_mocks` subpackage (`repos/repo_mocks`, `services/service_mocks`), never in the production package.

## Style

- Comments minimal: one line per exported identifier, short package doc; no README/ADR refs in new code.
- No speculative code, options, or interfaces nothing uses.
- gofmt + goimports with local prefix `github.com/bete7512/hookbeam` (`make fmt`).
- Makefile targets, hyphenated; no Taskfile.

## Definition of done

1. `make gen-openapi` if the spec changed.
2. `go generate ./api/...` for touched interfaces (or `make gen-mocks`).
3. `make fmt`
4. `make lint`
5. `make test`
6. `make test-integration` if repos or migrations changed.
7. `make gen-check` (regenerates and fails if any generated file changed).
8. New endpoint: live curl check against `make dev` (see `verify-change` skill).

## Do not

- No ORM, sqlc, or SQL builders; no strict server.
- No SQL outside `<service>/repos`: not in `pkg/`, `jobs/`, `services/` or `handlers/`. A new table gets a repo, even an infrastructure table.
- No publishing to the queue from services or handlers; write an outbox row and let the relay publish.
- No per-request counters, sessions-like hot state or caches in Postgres; use `Core.Cache` (`pkg/cache`), never a second cache instance or a package-level map.
- No raw action tokens in outbox payloads, logs or the database; payloads carry ids, the worker issues the token, only its hash is stored.
- No "email not found" answers on `forgot-password` or `resend-verification`; always 202.
- No `git add`, `git commit` or `git push` unless the user explicitly asks for it.
- No client input in `ORDER BY` or any SQL string; sort keys go through `sorting.Columns`, values through `pgx.NamedArgs`.
- No UUID primary keys; ids are bigint identity / `int64`.
- No routes outside the spec (only `/openapi.json`, `/docs`, and the `/` 404 are added in `router.go`).
- No logging in services or repos; no global loggers/pools/config.
- No `panic` in request paths.
- No hand edits under `<service>/gen/`.
- No imports across services (`api` → `delivery/...`), no shared tables between services.
- No migrations at app boot.
- No `x-` custom spec extensions for auth; auth reads `security`.
- No token parsing or `Authorization` header checks in handlers or services; the middleware owns it, handlers read `auth.ClaimsFrom`.
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

- Auth, remaining: setting `access_token` cookies on login and the redirect after the Google callback (with the frontend, Day 24); listing and unlinking identities; a second OIDC provider (config + two routes).
- Authz (Day 16): `Authorizer` interface, RBAC adapter + OpenFGA adapter.
- Events, remaining: NATS or other transport adapters, payload versioning, worker autoscaling on queue depth (Day 14).
- Observability: OTel, metrics, Sentry/GlitchTip, dashboards, runbooks.
- Infra: Terraform, Ansible, ECS deploy workflow, gRPC (`gen-proto` is a stub). Built: one Dockerfile per binary next to its `main.go` (`api/cmd/{api,worker}/Dockerfile`, distroless, non-root, built from the repo root) and `.github/workflows/ci.yml` (lint, gen-check, unit + integration against a Postgres service and LocalStack, govulncheck, image build + Trivy).
