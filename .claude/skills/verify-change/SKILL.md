---
name: verify-change
description: Use before reporting ANY code change as done in this repo, and always after adding or changing an endpoint, OpenAPI spec, migration, repo, interface, or error handling. Runs the quality gates in order, then a live curl + access-log check against a real Postgres.
---

# verify-change

Never claim done with a red or skipped-but-relevant gate.

## 1. Gates (in order; stop and fix at first red)

| # | When | Command | Green |
|---|---|---|---|
| 1 | spec under `api/openapi/` touched | `make lint-openapi && make gen-openapi` (`APP=<name>` for another app) | exit 0; `api/gen/openapi/*` updated |
| 2 | an interface with `//go:generate` changed | `go generate ./api/...` | exit 0; `mock_*.go` updated |
| 3 | always | `make fmt` | exit 0 (may rewrite files) |
| 4 | always | `go build ./... && go vet ./... && go vet -tags integration ./...` | no output |
| 5 | always | `make lint && golangci-lint run --build-tags integration ./...` | `0 issues.` twice |
| 6 | always | `make test` | all `ok`, no `FAIL` |
| 7 | repos, migrations, `pkg/db`, `pkg/testutil` touched | `make test-integration` | all `ok` |
| 8 | always | `make gen-check` | exit 0 |

- 7 needs Docker: testcontainers starts `postgres:16-alpine` unless `TEST_DATABASE_URL` is set, in which case each package creates its own database on that server (needs CREATEDB, e.g. the superuser) and drops it after.
- 8: `gen-check` hashes generated files under the service folders, runs `make gen`, and fails if anything changed.
  A failure means generated code was stale; it has been regenerated, so review the diff. Never stage or commit to make it pass.
- Migrations additionally: `make migrate-up && make migrate-down && make migrate-up` (see `add-migration`).
- Integration tests are unseen by `go build`/`make lint` without the tag — never skip 4/5's tagged half.

## 2. Live check

Run when an endpoint, middleware, error mapping, or migration changed. Edit `PORT` and the curls.

```bash
cd "$(git rev-parse --show-toplevel)"
set -a; source <(sed -E 's/[[:space:]]+#.*$//' .env); set +a   # strip inline comments
echo "db: $POSTGRES_USER@localhost:$POSTGRES_PORT/$POSTGRES_DB"  # read, don't assume hookbeam/5433
# AUTH_SECRET_KEY, AUTH_ENCRYPTION_KEY and AUTH_JWT_PRIVATE_KEY are required at boot and for migrate;
# .env.example ships them empty, so fill .env first: go run ./scripts/setup-env
APP=api
make db-up && make migrate-up APP=$APP && make build APP=$APP
PORT=18080; LOG=$(mktemp)
HTTP_ADDR=":$PORT" LOG_FORMAT=json ./bin/$APP/api >"$LOG" 2>&1 &
API_PID=$!
for i in $(seq 50); do curl -sf "localhost:$PORT/healthz" >/dev/null && break; sleep 0.2; done
B="localhost:$PORT"; H='-H Content-Type:application/json'
c() { curl -s -w ' [%{http_code}]\n' "$@"; }
field() { python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$1"; }
c "$B/healthz"
c "$B/readyz"
# goauth routes (bodies are goauth's: {"data": {...}})
c -X POST $H -d '{"email":"live@example.com","name":"Live","password":"Correct-horse-9"}' "$B/v1/auth/signup"   # 201
c -X POST $H -d '{"email":"live@example.com","name":"Live","password":"Correct-horse-9"}' "$B/v1/auth/signup"   # 409
c -X POST $H -d '{"email":"live@example.com","password":"wrong-password"}' "$B/v1/auth/login"                   # 401
LOGIN=$(curl -s -c /tmp/jar -X POST $H -d '{"email":"live@example.com","password":"Correct-horse-9"}' "$B/v1/auth/login")
TOKEN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["access_token"])' <<<"$LOGIN")
REFRESH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["data"]["refresh_token"])' <<<"$LOGIN")
# ours
c "$B/v1/me"                                                                     # 401: no token
c -H "Authorization: Bearer $TOKEN" "$B/v1/me"                                   # 200: userId, workspaceId, methods
c -b /tmp/jar "$B/v1/me"                                                         # 200: goauth_access_hookbeam cookie
c -H "Authorization: Bearer $TOKEN" "$B/v1/auth/me"                              # 200: profile
c -X POST $H -d "{\"refresh_token\":\"$REFRESH\"}" "$B/v1/auth/refresh"          # 200: rotated
c -X POST $H -d "{\"refresh_token\":\"$REFRESH\"}" "$B/v1/auth/refresh"          # 401: reuse
c "$B/v1/auth/.well-known/jwks.json"                                             # 200: ES256 key with kid
for i in $(seq 1 11); do c -X POST $H -d '{"email":"live@example.com","password":"wrong"}' "$B/v1/auth/login"; done  # 11th → 429 + Retry-After (10 per identifier per 10 min)
c "$B/v1/auth/oauth/providers"                                                   # 200: [] without OIDC_GOOGLE_CLIENT_ID
kill -TERM "$API_PID"; wait "$API_PID"; echo "exit=$?"
python3 - "$LOG" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    try: e = json.loads(line)
    except ValueError: print("NON-JSON:", line.rstrip()); continue
    if e.get("msg") != "http request": continue
    print(f'{e["level"]:5} {e["status"]} {e["method"]} route={e["route"]!r} path={e["path"]} error={e.get("error","")}')
PY
make db-down
```

- Port 5432 is often a host Postgres; compose maps `${POSTGRES_PORT}:5432`. If `make db-up` fails
  on bind or `make migrate-up` hits the wrong server, check `ss -ltnp | grep 543` and `.env`.
- Take real paths/bodies from `api/openapi/paths/*.yaml` (prefix in `api/openapi/openapi.yaml`);
  the curls above are examples.
- Protected ops (spec `security` is not `[]`) need `-H "Authorization: Bearer $TOKEN"` from the login above; without it expect `401 UNAUTHORIZED`.
- Boot fails with `AUTH_SECRET_KEY must be at least 32 characters` (or the encryption/private key line) if setup-env was skipped.
- `exit=0` after SIGTERM = graceful shutdown worked.

## 3. What to assert

- **Status** matches the spec's responses for each case (201/200, 400, 404, 409, 422, 500).
- **Error body** is exactly `{"error","message","code","fields"?,"request_id"}`: `error` is the
  UPPER_SNAKE api code, `code` equals the HTTP status, `fields` only on validation, `request_id`
  equals the `X-Request-ID` response header.
- **500s leak nothing**: generic message only; no SQL, pgx, stack, or wrapped cause text in the body.
- **Access log**: exactly one `http request` line per curl; `route` is the pattern
  (`GET /v1/me`), never empty; `path` holds the concrete URL. `route="/"` means the
  request fell through to the catch-all 404 — the route is not registered (spec/gen/handler gap).
- **Levels**: `<500` → `INFO`; `>=500` → `ERROR` with `error` attr carrying the cause.
- No handler-level "request received" lines; no secrets/passwords/tokens in any log line.
- **Auth**: failed login is 401 whether or not the email exists; a reused refresh token is 401; a new protected route without a token is 401 `UNAUTHORIZED` in our error shape; goauth routes answer in goauth's shape. The 11th credential check per identifier in 10 minutes (or the 31st per IP in a minute) is 429 with `Retry-After` and `Ratelimit-*` headers; windows are clock-aligned, so a loop that straddles a boundary can see fewer 429s.
- Rate-limit counters live in the api process's cache: restarting the api resets them, and with several replicas each one counts separately.
- Migration changes: `make migrate-status` shows the new version; affected endpoints return
  rows including the new columns.

## 4. Report

- List each gate run with pass/fail; name gates skipped and why (e.g. "7 skipped: no repo change").
- Paste only failing output (trimmed to the relevant lines), plus the live-check summary table.
- Red gate → say so and fix or hand back; never "done" with a failure outstanding.
