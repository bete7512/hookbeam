# Quick start: the whole product with Docker Compose

Everything runs in containers: Postgres, LocalStack (SNS and SQS), Mailpit, the control plane (`api` and its
worker), the data plane (`delivery` server and worker), the demo webhook receiver, the console and a proxy that
serves console and API on one origin. Nothing but Docker is needed on the machine.

## Prerequisites

- Docker Engine 24 or later with Compose v2 (`docker compose version`).
- Ports 3000 (console and API), 5433 (Postgres), 4566 (LocalStack), 8025 (Mailpit), 8080 (API) and 8081 (receiver) free.
- About 4 GB of RAM for the containers and a few minutes for the first image build.

## Start

```sh
git clone https://github.com/bete7512/hookbeam.git
cd hookbeam
docker compose up -d --build --wait      # or: make demo-up
```

The first start builds six images and takes a few minutes. `bootstrap` writes the keys and the mTLS certificates
into the `secrets` volume once, `db-init` creates the data plane's database, `api-migrate` and `delivery-migrate`
apply the migrations, then the long-running services come up. `docker compose ps` shows the one-shot services as
exited (0) and the rest running.

Open the console at <http://localhost:3000>.

## First delivery

1. **Sign up** at <http://localhost:3000/signup>. The workspace named after you appears a moment later; the welcome
   email lands in Mailpit at <http://localhost:8025>.
2. **Create an application** under Applications, then **register an endpoint** at `http://receiver:8081/hooks`
   subscribed to `order.shipped`. The console shows the signing secret once; copy it.
3. **Give the receiver the secret** so it can verify signatures:

   ```sh
   RECEIVER_SECRET=whsec_... docker compose up -d receiver
   ```

   Until then the receiver answers 401 and the attempt's response preview says so; a replay after the restart succeeds.
4. **Create a key** bound to the application (shown once) and **submit an event**:

   ```sh
   curl -X POST http://localhost:3000/v1/applications/<application id>/messages \
     -H "Authorization: Bearer hb_..." \
     -H "Idempotency-Key: order-817-shipped-v1" \
     -H "Content-Type: application/json" \
     -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}'
   ```

   The Events page lists it; the delivery shows a succeeded attempt with `HTTP 204`, and `docker compose logs
   receiver` shows "verified and applied". The endpoint page's **Send test delivery** does the same without a key.
5. **Watch a retry**: edit the endpoint URL to `http://receiver:8081/hooks?status=503`, send a test delivery, and the
   attempt is retried on the schedule (15 s, 1 m, 5 m, ...). `?status=400` fails for good; `?delay=15s` times out.

`make demo-smoke` runs steps 1 to 4 through the API and asserts the succeeded attempt (`scripts/demo-smoke.sh`).

## What runs where

| Service | Port | Role |
|---|---|---|
| `proxy` | 3000 | one origin: `/v1/*`, `/openapi.json`, `/docs`, `/healthz`, `/readyz` to the API, everything else to the console |
| `console` | (3001 inside) | Next.js console |
| `api` | 8080 | control plane: identity, workspaces, keys, public REST, `/docs` |
| `api-worker` | | outbox relay, health notifications, emails |
| `delivery` | (9090 inside, mTLS) | data plane gRPC: applications, endpoints, messages, deliveries |
| `delivery-worker` | | claims due deliveries, signs, posts, records attempts |
| `receiver` | 8081 | demo webhook receiver; `RECEIVER_SECRET` from the endpoint |
| `postgres` | 5433 | two databases: `hookbeam` and `hookbeam_delivery` |
| `localstack` | 4566 | SNS topic and the two SQS queues the workers use |
| `mailpit` | 8025 | every email the stack sends |

Logs: `docker compose logs -f api api-worker delivery delivery-worker receiver` (or `make demo-logs`).

## Stop and reset

```sh
docker compose down          # stop; data and secrets volumes are kept   (make demo-down)
docker compose down -v       # stop and delete data and secrets          (make demo-reset)
```

## Development notes

- The stack is development configuration: `APP_ENV=dev`, cookies over plain http, and
  `DELIVERY_SENDER_ALLOW_PRIVATE=true` so the worker may post to the receiver's private address over http. The data
  plane refuses that setting with `APP_ENV=staging` or `prod`, where every destination must be public https.
- Secrets live in files in the `secrets` volume (`AUTH_SECRET_KEY_FILE`, `AUTH_ENCRYPTION_KEY_FILE`,
  `AUTH_JWT_PRIVATE_KEY_FILE`, `DELIVERY_SECRETS_KEY_FILE`); the certificates are a local CA with one server and one
  client certificate. `docker compose down -v` discards them, and the next start generates new ones.
- `.env` in the repository root is read by Compose for `POSTGRES_*` and `RECEIVER_SECRET` only; the host-side
  development loop (`make dev`, `make run-delivery`, `make console-dev`) reads the rest of it.
