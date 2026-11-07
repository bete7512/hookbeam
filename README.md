# scaffold

A Go backend template for building production services: a spec-first HTTP API on PostgreSQL, hand-written SQL, consistent error handling, and conventions that both people and AI assistants can follow.

## What works today

- **Users API:** register, get, list with paging, search and sorting, update, change password, delete. Authenticated reads and mutations are limited to your own user; the list is also scoped to you.
- **Authentication:** login with short-lived Ed25519-signed JWTs, refresh with rotation and reuse detection, logout, logout everywhere, `/v1/me`. Email verification and password reset through single-use links sent by the worker. Google login through OpenID Connect with PKCE, existing links resolve by provider/subject. Automatic linking requires Google authority over the verified address (Gmail or a signed hosted-domain claim); third-party address collisions require an explicit linking flow, which is not implemented.
- **Rate limits:** independent per-IP budgets for login, registration, recovery, action tokens, OIDC and sessions; per-account login limits and recovery cooldowns. Atomic bounded counters live in the shared process cache, retain active windows, return 429 with `Retry-After`, and fail closed with 503 at capacity. Counters reset on restart and are not shared across replicas. Tokens are accepted as a bearer header or an `access_token` cookie, and routes are protected by the spec's `security`.
- **Health checks:** `/healthz` for liveness and `/readyz`, which pings the database.
- **API docs:** `/openapi.json` and an interactive reference at `/docs`.
- **Errors:** one JSON error shape with a request id, and internal details never leak to clients.
- **Logging:** one structured log line per request, with the error attached when a request fails.
- **Background jobs:** registering a user queues a welcome email through a transactional outbox, SNS and SQS. Events are written in the same transaction as the data that caused them, successful processing is recorded to suppress replay, and failed or unknown messages remain for redrive to the configured dead-letter queue. SQS visibility is renewed during bounded work. External effects such as email may repeat after a crash or ambiguous send result. Locally this runs on LocalStack and Mailpit.
- **Email:** SMTP (Mailpit in development) or Amazon SES.
- **Database:** Go migrations compiled into the binary.
- **Tests:** unit tests with mocks, and integration tests against a real Postgres.
- **Shipping:** one distroless, non-root Docker image per binary, and a GitHub Actions pipeline that lints, checks generated code, runs unit and integration tests, scans for vulnerabilities and builds the images.

Planned next: organization roles and tenant authorization, observability, a Next.js frontend, and deployment to AWS ECS with Terraform.

## Stack

| Area | Choice |
|---|---|
| Language | Go 1.26 |
| HTTP | Standard library `net/http` router |
| API contract | OpenAPI 3.0.3, generated with `oapi-codegen` |
| Database | PostgreSQL 16, `pgx` v5, hand-written SQL |
| Migrations | `goose`, written as Go files |
| Logging | `log/slog`, JSON |
| Passwords | argon2id |
| Tokens | EdDSA JWT access tokens, opaque rotating refresh tokens, hashed single-use action tokens |
| Social login | OpenID Connect with PKCE (`go-oidc`), Google first |
| Events | Transactional outbox, SNS + SQS (`aws-sdk-go-v2`), LocalStack locally |
| Email | SMTP (Mailpit locally) or SES |
| Tests | `testify`, `gomock`, `testcontainers` |
| Tooling | Makefile, Docker Compose, `golangci-lint` |

## Quick start

You need Go, Docker and Make.

```bash
make setup        # install tools and create .env from .env.example
make db-up        # start Postgres with Docker Compose
make migrate-up   # create the tables
make run-api      # serve on http://localhost:8080, docs at /docs
```

`make setup` creates independent `AUTH_JWT_SIGNING_KEY` and `AUTH_ACTION_TOKEN_KEY` values when absent or blank and preserves existing non-empty keys. Both must decode to 32 bytes. Existing checkouts can run `go run ./scripts/setup-env`. Store the action key durably and share it between API and worker: email retries derive the same link from it.

Staging/production require AWS events without endpoint overrides, SES, a non-empty issuer, and public HTTPS link/Google callback URLs. These validation gates are only a baseline; see [architectural-defeit.md](architectural-defeit.md) for remaining release requirements.

Migrations 0007–0008 invalidate old refresh sessions and action links. Apply migrations before deploying the matching API and worker together; users must sign in again or request a new email. Do not run old and new authentication implementations together during this security upgrade.

For the welcome email, start the queue and the worker too:

```bash
make queue-up     # LocalStack (SNS + SQS) and Mailpit
make run-worker   # relays the outbox and sends the email; inbox at http://localhost:8025
```

Try it (needs `jq`):

```bash
curl -X POST localhost:8080/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","name":"Ada","password":"sdfkjhjasdfgakdsf"}'

TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"sdfkjhjasdfgakdsf"}' | jq -r .accessToken)

curl localhost:8080/v1/me -H "Authorization: Bearer $TOKEN"

curl 'localhost:8080/v1/users?limit=10&sort_by=email&sort_dir=asc' -H "Authorization: Bearer $TOKEN"
```

## Layout

```
apps/api/          the current app; see apps/README.md for why apps are shaped this way
  cmd/api/             HTTP server (also runs migrations with -m migrate)
  cmd/worker/          background worker: outbox relay, cleanup, event handlers
  app/ config/ …       handlers, services, repos, models, events, jobs, config, wiring
  migrations/          database schema
  openapi/             API contract
  gen/                 generated code, never edited by hand
pkg/                   shared libraries with no business logic: errors, HTTP, database, config, logging, sorting, auth, events, mailer
deploy/                LocalStack bootstrap for local SNS/SQS
scripts/               code generation helpers
architectural-defeit.md current architecture decisions, limits and prioritized debt
.claude/               conventions and skills for Claude Code
```

## How a request flows

1. Middleware assigns a request id, starts the access log and recovers panics.
2. The generated router matches the route and parses path and query parameters.
   Auth middleware checks the token if the route's `security` requires one.
3. The handler decodes the body into a generated type and calls a service.
4. The service validates input and calls a repository.
5. The repository runs SQL through `pgx`.
6. On success the handler writes JSON. On failure it calls `httpx.WriteError`, which picks the status code and adds the error to the request's log line.

## Conventions

- **The spec comes first.** A route exists only if it's in `apps/<name>/openapi/`. Run `make gen-openapi`, and the build fails until a handler implements it.
- **Each layer has one job.** Handlers don't touch SQL, services don't know about HTTP, and repos hold no business rules.
- **Errors use `pkg/apperr`.** Repos wrap errors with sentinels, services turn them into errors that carry a status, and handlers only call `WriteError`.
- **No ORM.** Queries are plain SQL with named arguments. Sort keys go through a whitelist, so client input never becomes SQL.
- **Tests are table-driven.** Repos use a real database, and services and handlers use generated mocks.
- **Side effects go through the outbox.** A service never talks to the queue; it writes an event row in its transaction, and the worker publishes and handles it.

The working guide is [AGENTS.md](AGENTS.md); shared playbooks also live under [`.claude/`](.claude/CLAUDE.md).

## Common commands

| Command | Does |
|---|---|
| `make run-api` / `make run-worker` | Run a process from source |
| `make migrate-up` / `migrate-down` / `migrate-status` | Manage the schema |
| `make gen` | Regenerate OpenAPI code and mocks |
| `make lint` | Run `golangci-lint` |
| `make test` | Run unit tests |
| `make test-integration` | Run tests against Postgres (needs Docker) |
| `make gen-check` | Fail if generated code is out of date |
| `make queue-up` / `make queue-down` | Start or stop LocalStack and Mailpit |
| `make images` | Build the api and worker Docker images |
| `make help` | List every target |

Commands that act on an app default to `apps/api`. Pick another with `APP=<name>`.

## Working with coding agents

The repository includes skills in `.claude/skills/` for common tasks:

- **add-endpoint:** a new operation or a whole new resource, from spec to tests.
- **add-list-endpoint:** paging, search and sorting.
- **add-migration:** schema changes, including zero-downtime ordering.
- **add-job:** a new event and its worker handler.
- **verify-change:** every quality gate plus a live check against Postgres.

## Decisions and roadmap

[architectural-defeit.md](architectural-defeit.md) records current guarantees, remaining architectural debt, upgrade notes, and verification results. [review.md](review.md) preserves the original review with its current disposition. [plans.md](plans.md) is the implementation roadmap.

[Hookbeam](product/README.md) is the proposed open-source webhook delivery product built on this foundation. Its delivery service, console and production infrastructure are planned work, not existing features. The private `.dev/` design draft is historical and is not required to understand the public repository.
