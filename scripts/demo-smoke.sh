#!/usr/bin/env bash
# Smoke test of the compose stack (make demo-up): through the proxy on :3000 it creates an account and a workspace,
# an application, an endpoint at the demo receiver, gives the receiver the endpoint's secret, submits one event with
# a key and waits for a succeeded attempt. Needs curl, jq and docker compose.
set -euo pipefail

base=${BASE_URL:-http://localhost:3000}
stamp=$(date +%s)-$RANDOM
email="smoke-$stamp@example.com"
password="Passw0rd!long"

say() { printf '\n== %s\n' "$*"; }
api() { curl -sS "$@"; }

say "readiness"
api -f "$base/readyz" | jq .

say "account and workspace"
api -f -X POST "$base/v1/auth/signup" -H 'content-type: application/json' \
  -d "{\"name\":\"Smoke $stamp\",\"email\":\"$email\",\"password\":\"$password\"}" > /dev/null
token=$(api -f -X POST "$base/v1/auth/login" -H 'content-type: application/json' \
  -d "{\"email\":\"$email\",\"password\":\"$password\"}" | jq -r .data.access_token)
org=""
for _ in $(seq 1 30); do
  org=$(api -f "$base/v1/auth/org/my" -H "Authorization: Bearer $token" | jq -r '.data[0].id // empty')
  [ -n "$org" ] && break
  sleep 1
done
[ -n "$org" ] || { echo "no workspace appeared after signup" >&2; exit 1; }
token=$(api -f -X POST "$base/v1/auth/org/switch" -H 'content-type: application/json' \
  -H "Authorization: Bearer $token" -d "{\"org_id\":\"$org\"}" | jq -r .data.access_token)
echo "workspace $org"

say "application, endpoint at the receiver, secret handed to the receiver's control page"
app=$(api -f -X POST "$base/v1/applications" -H 'content-type: application/json' \
  -H "Authorization: Bearer $token" -d '{"name":"Smoke"}' | jq -r .id)
endpoint=$(api -f -X POST "$base/v1/applications/$app/endpoints" -H 'content-type: application/json' \
  -H "Authorization: Bearer $token" -d '{"url":"http://receiver:8081/hooks","eventTypes":["order.shipped"]}')
secret=$(echo "$endpoint" | jq -r .signingSecret)
echo "application $app, endpoint $(echo "$endpoint" | jq -r .endpoint.id)"
api -f -X POST "${RECEIVER_URL:-http://localhost:8081}/control" -H 'Accept: application/json' --data-urlencode "secret=$secret" --data-urlencode "scenario=ok" | jq -c '{scenario, has_secret}'

say "submit one event with a bound key"
key=$(api -f -X POST "$base/v1/applications/$app/api-keys" -H 'content-type: application/json' \
  -H "Authorization: Bearer $token" -d '{"name":"smoke","scopes":["messages:write","messages:read"]}' | jq -r .key)
message=$(api -f -X POST "$base/v1/applications/$app/messages" -H "Authorization: Bearer $key" \
  -H 'content-type: application/json' -H "Idempotency-Key: smoke-$stamp" \
  -d '{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}' | jq -r .messageId)
echo "message $message"

say "waiting for the delivery to succeed"
state=""
for _ in $(seq 1 60); do
  state=$(api -f "$base/v1/messages/$message" -H "Authorization: Bearer $key" | jq -r '.deliveries[0].state')
  case "$state" in succeeded|failed|cancelled) break ;; esac
  sleep 1
done
delivery=$(api -f "$base/v1/messages/$message" -H "Authorization: Bearer $key" | jq -r '.deliveries[0].id')
api -f "$base/v1/deliveries/$delivery/attempts" -H "Authorization: Bearer $key" | jq '.items[] | {number, statusCode, errorClass, latencyMs}'
if [ "$state" != "succeeded" ]; then
  echo "delivery ended as $state" >&2
  exit 1
fi
echo "delivery $delivery succeeded"
say "the receiver applied it once"
api -f "${RECEIVER_URL:-http://localhost:8081}/state" | jq '{counter, applied: [.records[] | select(.applied)] | length}'
