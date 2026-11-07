# Hookbeam

**Deliver webhooks. Inspect every attempt. Recover failures.**

Hookbeam is an open-source webhook delivery platform for software teams. Your application submits an event once; Hookbeam stores it, delivers signed requests to every subscribed endpoint, retries temporary failures with backoff, and keeps the full attempt history so "did you send this?" has an answer.

> **Status:** backend foundation. The control plane (identity, workspaces, outbox, worker, email) runs today; the delivery service, console and AWS deployment are the next milestones. The [product specification](product/README.md) is the contract for what ships; the [debt register](architectural-defeit.md) is the honest gap list.

## Architecture

![Hookbeam architecture: sender, control plane API, delivery service, workers, databases, bus and customer endpoint](docs/architecture.svg)

One event, end to end:

1. Your backend posts the event with an API key and an idempotency key.
2. The Hookbeam API, the control plane, authenticates the key, resolves the workspace, and calls the delivery service over gRPC.
3. The delivery service writes the message, one delivery per subscribed endpoint, and an outbox row in a single transaction.
4. The delivery worker claims due deliveries from Postgres with `FOR UPDATE SKIP LOCKED`, under a fenced lease.
5. It signs the payload, posts it over bounded HTTPS, and records the response, latency and next attempt. A `2xx` acknowledges; anything else is retried with jittered backoff until the retry budget ends.
6. State changes flow through SNS and SQS to the control plane worker, which raises notifications and updates dashboard projections.
7. Email leaves through SES or SMTP. The console shows every attempt and lets an authorized user replay one.

Two rules hold the design together. Every side effect starts as an outbox row in the same transaction as the data that caused it, so nothing publishes to the bus from a request handler. The control plane and the data plane each own their database, and they talk only through the gRPC contract and events on the bus.

## Built today

| Area | What exists |
|---|---|
| Control plane API | Spec-first OpenAPI on the standard library router, hand-written SQL through `pgx`, one JSON error shape with a request id, one log line per request |
| Identity | [goauth](https://github.com/bete7512/goauth), the author's authentication library, mounted at `/v1/auth`: signup, login, refresh rotation with reuse detection, logout, email verification, password reset, Google login, per-IP and per-account rate limits, ES256 tokens with a JWKS endpoint, cookies for the console |
| Workspaces | goauth organizations: one per signup, invitations, owner/admin/member roles, `org/switch` selects the active workspace for a session |
| API keys | Workspace-scoped sender credentials under `/v1/api-keys`: hashed at rest, prefix shown, scopes, expiry, revocation, last-used tracking; accepted as `Authorization: Bearer hb_...` on routes that declare it |
| Events | Transactional outbox relayed to SNS and SQS, exactly-once handling per event, dead-letter queue after three failures. LocalStack and Mailpit locally |
| Worker | Second binary over the same code: outbox relay, outbox retention, event handlers |
| Migrations | goose migrations compiled into the binary plus goauth's own, both run by one command as a one-off task, never at boot |
| Shipping | One distroless, non-root image per binary; CI runs lint, generated-code drift, unit and integration tests, vulnerability scan and image build |

## Stack

Go 1.26 · PostgreSQL 16 · `net/http` · OpenAPI 3 with oapi-codegen · pgx v5 · goose · log/slog · SNS + SQS · SES or SMTP · gRPC between services · Next.js console · Docker Compose locally, AWS ECS in production.

## Quick start

You need Go 1.26, Docker and Make.

```bash
make setup        # install tools, create .env with generated keys
make db-up        # Postgres on :5433
make migrate-up   # apply the schema
make run-api      # http://localhost:8080, interactive docs at /docs
```

For the worker, the queue and email delivery:

```bash
make queue-up     # LocalStack (SNS + SQS) and Mailpit at http://localhost:8025
make run-worker   # relays the outbox and handles events
```

Try it:

```bash
curl -X POST localhost:8080/v1/auth/signup \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","name":"Ada","password":"Correct-horse-battery-9"}'

TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"Correct-horse-battery-9"}' | jq -r .data.access_token)

curl localhost:8080/v1/me -H "Authorization: Bearer $TOKEN"

# pick the workspace signup created, then mint a sender key
ORG=$(curl -s localhost:8080/v1/auth/org/my -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/org/switch -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d "{\"org_id\":\"$ORG\"}" | jq -r .data.access_token)
KEY=$(curl -s -X POST localhost:8080/v1/api-keys -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"name":"backend"}' | jq -r .key)

curl localhost:8080/v1/me -H "Authorization: Bearer $KEY"   # methods: ["api_key"], workspaceId set
```

The welcome email lands in Mailpit.

## Repository

```
api/          control plane: identity, workspaces, API keys, public REST; cmd/{api,worker}
delivery/     data plane: delivery service and worker (planned, milestone 1)
console/      Next.js console (planned)
pkg/          domain-free libraries: errors, HTTP, database, config, logging, events, mailer, cache
deploy/       LocalStack bootstrap
docs/         architecture diagram, ADRs, runbooks
product/      the Hookbeam product specification
```

Each service owns its data, migrations, spec and config; `pkg/` never imports a service, enforced by lint. [ADR 0003](docs/adr/0003-single-go-module.md) explains the layout.

## Development

| Command | Does |
|---|---|
| `make test` | unit and handler tests, no Docker |
| `make test-integration` | everything, against a real Postgres in testcontainers |
| `make lint` | golangci-lint |
| `make gen` | regenerate OpenAPI code and mocks |
| `make gen-check` | fail if generated code is stale |
| `make images` | build the api and worker images |
| `make help` | every target |

Targets that act on a service default to `api`; pick another with `APP=<name>`.

Conventions, layer rules and testing patterns live in [AGENTS.md](AGENTS.md); coding agents also get task playbooks under [`.claude/skills`](.claude/skills) for endpoints, list endpoints, migrations, jobs and the verification gate.

## Decisions and roadmap

- [product/README.md](product/README.md): scope, delivery contract, security requirements, milestones.
- [architectural-defeit.md](architectural-defeit.md): current guarantees and prioritized debt.
- [docs/adr](docs/adr): architecture decision records.
- [plans.md](plans.md) and [review.md](review.md): the implementation plan and the review it answers.
