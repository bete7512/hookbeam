# Hookbeam

Open-source webhook delivery. Your application submits an event once; Hookbeam stores it, posts a signed request to every subscribed endpoint, retries failures with backoff, and keeps every attempt so "did you send this?" has an answer.

The [specification](specifications/README.md) defines the product. The [decisions](docs/decisions) explain the architecture.

## How it works

![Architecture: sender, control plane API, delivery service, workers, databases, bus, customer endpoint](docs/architecture.svg)

1. Your backend posts an event with an API key and an `Idempotency-Key`.
2. The control plane (`api/`) authenticates the key, resolves the workspace and calls the delivery service over gRPC with mTLS.
3. The delivery service (`delivery/`) writes the message, one delivery per subscribed endpoint and a wake-up row in one transaction.
4. The delivery worker claims due deliveries from Postgres under a lease, signs the body (Standard Webhooks), posts it through an SSRF-guarded client and records the attempt and the next state in one transaction.
5. A `2xx` succeeds. Other failures retry at 15s, 1m, 5m, 30m, 2h, 6h, 12h with jitter, up to eight attempts within 24 hours; `Retry-After` is honoured.
6. Every message, delivery and attempt is readable back through the API.

The two planes own separate databases and talk only through the gRPC contract in `contracts/` and events on the bus.

## What exists

- Identity and workspaces through [goauth](https://github.com/bete7512/goauth) at `/v1/auth`: signup, login, refresh, verification, reset, Google, organizations as workspaces with owner/admin/member roles.
- Workspace API keys with scopes (`messages:write`, `messages:read`), hashed at rest, revocable.
- Applications, endpoints with encrypted signing secrets and event-type subscriptions, pause.
- Message acceptance, fan-out, signed delivery, retries, attempt history, a demo receiver that verifies signatures.
- Transactional outbox over SNS/SQS with idempotent consumers and a dead-letter queue.
- One distroless image per binary; CI with lint, generated-code drift, unit and integration tests, vulnerability scan.

Not yet: replay and cancel, lease reconciliation, secret rotation, endpoint health and notifications, the console, Terraform and the AWS deployment. Open work is tracked as [GitHub issues](https://github.com/bete7512/hookbeam/issues) by milestone.

## Quick start

Needs Go 1.26, Docker and Make.

```bash
make setup                    # tools, .env with generated keys, local mTLS certificates
make db-up                    # Postgres on :5433 with both databases
make migrate-up               # control plane schema
make migrate-up APP=delivery  # data plane schema
make run-delivery             # gRPC on :9090
make run-worker APP=delivery  # delivery worker
make run-api                  # http://localhost:8080, docs at /docs
```

Send one event end to end:

```bash
curl -X POST localhost:8080/v1/auth/signup -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","name":"Ada","password":"Correct-horse-battery-9"}'

TOKEN=$(curl -s -X POST localhost:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"Correct-horse-battery-9"}' | jq -r .data.access_token)

# select the workspace signup created, then mint a sender key
ORG=$(curl -s localhost:8080/v1/auth/org/my -H "Authorization: Bearer $TOKEN" | jq -r '.data[0].id')
TOKEN=$(curl -s -X POST localhost:8080/v1/auth/org/switch -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d "{\"org_id\":\"$ORG\"}" | jq -r .data.access_token)
KEY=$(curl -s -X POST localhost:8080/v1/api-keys -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"name":"backend","scopes":["messages:write","messages:read"]}' | jq -r .key)

# an application, an endpoint at the demo receiver, and the receiver with the endpoint's secret
APP=$(curl -s -X POST localhost:8080/v1/applications -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"name":"Customer A"}' | jq -r .id)
SECRET=$(curl -s -X POST localhost:8080/v1/applications/$APP/endpoints -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"url":"http://localhost:8081/hooks","eventTypes":["order.shipped"]}' | jq -r .signingSecret)
RECEIVER_SECRET=$SECRET make run-receiver &

# submit; the receiver logs "verified and applied"
curl -s -X POST localhost:8080/v1/applications/$APP/messages -H "Authorization: Bearer $KEY" \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: order-817-shipped-v1' \
  -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}'

# read back the message with its deliveries, then one delivery's attempts
curl -s localhost:8080/v1/messages/1 -H "Authorization: Bearer $TOKEN" | jq .
curl -s localhost:8080/v1/deliveries/1/attempts -H "Authorization: Bearer $TOKEN" | jq .
```

An endpoint at `http://localhost:8081/hooks?status=503` shows the retry schedule. `make queue-up` starts LocalStack and Mailpit for the SNS/SQS path and emails.

## Repository

```
api/             control plane: identity, workspaces, API keys, public REST; cmd/{api,worker}
delivery/        data plane: applications, endpoints, secrets, messages, deliveries, attempts; cmd/{server,worker}
contracts/       gRPC contract between the planes (proto + generated Go)
pkg/             domain-free libraries
examples/        demo webhook receiver
deploy/          LocalStack bootstrap; local certificates
docs/            architecture diagram, decisions
specifications/  product specification
```

## Development

| Command | Does |
|---|---|
| `make test` | unit tests, no Docker |
| `make test-integration` | with a real Postgres in testcontainers |
| `make lint` | golangci-lint |
| `make gen` | regenerate OpenAPI code, the gRPC contract and mocks |
| `make gen-check` | fail if generated code is stale |
| `make help` | every target |

Targets that act on a service default to `api`; use `APP=delivery` for the data plane. Conventions and layer rules are in [`.claude/CLAUDE.md`](.claude/CLAUDE.md).
