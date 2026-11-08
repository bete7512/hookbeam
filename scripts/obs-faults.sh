#!/usr/bin/env bash
# Makes every alert rule fire on the local stack started with `make obs-up`, one fault at a time, and reports which
# rules Grafana shows as firing. Takes about twelve minutes because of the rules' `for` windows and the five-minute
# staleness of a stopped worker's gauge. Restores the stack at the end.
set -euo pipefail

base=${BASE_URL:-http://localhost:3000}
grafana=${GRAFANA_URL:-http://localhost:3002}
receiver=${RECEIVER_URL:-http://localhost:8081}
compose="docker compose -f docker-compose.yml -f docker-compose.observability.yml"
stamp=$(date +%s)-$RANDOM
password="Passw0rd!long"

say() { printf '\n== %s\n' "$*"; }
api() { curl -sS "$@"; }

# firing <rule title> [timeout seconds]: waits until Grafana reports the rule firing.
firing() {
  local title=$1 timeout=${2:-240} waited=0
  while [ $waited -lt "$timeout" ]; do
    if api -f "$grafana/api/prometheus/grafana/api/v1/rules" | jq -e --arg t "$title" '.data.groups[].rules[] | select(.name == $t and .state == "firing")' > /dev/null; then
      echo "FIRING  $title (after ${waited}s)"
      return 0
    fi
    sleep 10; waited=$((waited + 10))
  done
  echo "MISSING $title (not firing after ${timeout}s)"
  return 1
}

restore() {
  say "restoring"
  $compose start localstack api-worker delivery-worker > /dev/null 2>&1 || true
  api -s -X POST "$receiver/control" -d scenario=ok > /dev/null 2>&1 || true
}
trap restore EXIT

say "account, workspace, application, endpoint, key"
email="faults-$stamp@example.com"
api -f -X POST "$base/v1/auth/signup" -H 'content-type: application/json' -d "{\"name\":\"Faults $stamp\",\"email\":\"$email\",\"password\":\"$password\"}" > /dev/null
token=$(api -f -X POST "$base/v1/auth/login" -H 'content-type: application/json' -d "{\"email\":\"$email\",\"password\":\"$password\"}" | jq -r .data.access_token)
org=""; for _ in $(seq 1 30); do org=$(api -f "$base/v1/auth/org/my" -H "Authorization: Bearer $token" | jq -r '.data[0].id // empty'); [ -n "$org" ] && break; sleep 1; done
token=$(api -f -X POST "$base/v1/auth/org/switch" -H 'content-type: application/json' -H "Authorization: Bearer $token" -d "{\"org_id\":\"$org\"}" | jq -r .data.access_token)
app=$(api -f -X POST "$base/v1/applications" -H 'content-type: application/json' -H "Authorization: Bearer $token" -d '{"name":"Faults"}' | jq -r .id)
endpoint=$(api -f -X POST "$base/v1/applications/$app/endpoints" -H 'content-type: application/json' -H "Authorization: Bearer $token" -d '{"url":"http://receiver:8081/hooks","eventTypes":["order.shipped"]}')
secret=$(echo "$endpoint" | jq -r .signingSecret)
api -f -X POST "$receiver/control" --data-urlencode "secret=$secret" --data-urlencode "scenario=ok" > /dev/null
key=$(api -f -X POST "$base/v1/applications/$app/api-keys" -H 'content-type: application/json' -H "Authorization: Bearer $token" -d '{"name":"faults","scopes":["messages:write"]}' | jq -r .key)
submit() { api -f -X POST "$base/v1/applications/$app/messages" -H "Authorization: Bearer $key" -H 'content-type: application/json' -H "Idempotency-Key: $1" -d '{"type":"order.shipped","data":{"n":1}}' > /dev/null; }

say "fault 1: the receiver fails for good; six submissions"
api -f -X POST "$receiver/control" -d scenario=fail > /dev/null
for i in $(seq 1 6); do submit "fail-$stamp-$i"; done
firing "Repeated failures" 300

say "fault 2: the delivery worker stops; a submission waits unclaimed"
$compose stop delivery-worker > /dev/null
submit "stalled-$stamp"
firing "Oldest due age" 360
firing "Stalled workers" 360

say "fault 3: LocalStack stops; the next submission's wake-up cannot be published"
$compose start delivery-worker > /dev/null
$compose stop localstack > /dev/null
sleep 5
submit "outbox-$stamp"
firing "Outbox lag" 300

say "fault 4: the control plane worker stops; its gauge goes stale"
$compose stop api-worker > /dev/null
firing "Notification pipeline failing" 480

say "firing rules now"
api -f "$grafana/api/prometheus/grafana/api/v1/rules" | jq -r '.data.groups[].rules[] | "\(.state)\t\(.name)"'
