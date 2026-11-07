# Hookbeam

**Deliver webhooks. Inspect every attempt. Recover failures.**

Hookbeam is an open-source webhook delivery platform for software teams. Your application submits an event once; Hookbeam stores it, delivers signed requests to every subscribed endpoint, retries temporary failures with backoff, and keeps the full attempt history so "did you send this?" has an answer.

> **Status:** two services, one contract. The control plane (identity, workspaces, API keys, outbox, worker, email) and the data plane's foundation (schema, gRPC contract, applications, endpoints with encrypted signing secrets, mTLS between the planes) run today. The first end-to-end delivery, the console and the AWS deployment are the next milestones. The [product specification](specifications/README.md) is the contract for what ships.

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
| Delivery service | The data plane in its own database: applications, endpoints with versioned signing secrets encrypted at rest, subscriptions, and the schema for messages, deliveries and attempts that follows the specification's state model ([ADR 0021](docs/adr/0021-delivery-schedule-model.md)) |
| Contract | `hookbeam.delivery.v1` in `contracts/`, generated with buf; the control plane calls the data plane over mTLS from a local CA (`make certs`), every call scoped to a workspace through metadata, `/readyz` reports the connection |
| Migrations | goose migrations compiled into the binary plus goauth's own, both run by one command as a one-off task, never at boot |
| Shipping | One distroless, non-root image per binary; CI runs lint, generated-code drift, unit and integration tests, vulnerability scan and image build |

## Stack

Go 1.26 · PostgreSQL 16 · `net/http` · OpenAPI 3 with oapi-codegen · pgx v5 · goose · log/slog · SNS + SQS · SES or SMTP · gRPC between services · Next.js console · Docker Compose locally, AWS ECS in production.

## Quick start

You need Go 1.26, Docker and Make.

```bash
make setup                    # install tools, create .env with generated keys, local mTLS certificates
make db-up                    # Postgres on :5433 with the hookbeam and hookbeam_delivery databases
make migrate-up               # control plane schema
make migrate-up APP=delivery  # data plane schema
make run-delivery             # gRPC on :9090 behind mTLS
make run-api                  # http://localhost:8080, interactive docs at /docs; /readyz shows db and delivery
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
api/             control plane: identity, workspaces, API keys, public REST; cmd/{api,worker}
delivery/        data plane: applications, endpoints, secrets, deliveries; cmd/{server,worker}
contracts/       the gRPC contract between the planes (proto + generated Go)
console/         Next.js console (planned)
pkg/             domain-free libraries: errors, HTTP, gRPC, database, config, logging, events, mailer, cache, secrets
deploy/          LocalStack bootstrap; local certificates land here
docs/            architecture diagram, ADRs
specifications/  the Hookbeam product specification
```

Each service owns its data, migrations, spec and config; `pkg/` never imports a service, enforced by lint. [ADR 0003](docs/adr/0003-single-go-module.md) explains the layout and why it stays one Go module.

## Development

| Command | Does |
|---|---|
| `make test` | unit and handler tests, no Docker |
| `make test-integration` | everything, against a real Postgres in testcontainers |
| `make lint` | golangci-lint |
| `make gen` | regenerate OpenAPI code, the gRPC contract and mocks |
| `make gen-check` | fail if generated code is stale |
| `make images` | build the api and worker images (`APP=delivery` for the data plane) |
| `make help` | every target |

Targets that act on a service default to `api`; pick another with `APP=<name>`.

Conventions, layer rules and testing patterns live in [`.claude/CLAUDE.md`](.claude/CLAUDE.md); coding agents also get task playbooks under [`.claude/skills`](.claude/skills) for endpoints, list endpoints, migrations, jobs and the verification gate.

## Decisions

- [specifications/README.md](specifications/README.md): scope, delivery contract, security requirements, milestones.
- [docs/adr](docs/adr): architecture decision records.
