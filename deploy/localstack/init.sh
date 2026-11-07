#!/usr/bin/env bash
# Creates the SNS topic and the SQS queues the workers use locally. LocalStack runs it from
# /etc/localstack/init/ready.d once its services are up; every step is safe to repeat.
# Each worker has its own queue (plus a dead-letter queue) subscribed to the one topic with a filter on the
# "type" message attribute the publisher sets: the control plane gets every delivery state event, the data
# plane only its wake-ups.
set -euo pipefail

topic_arn=$(awslocal sns create-topic --name hookbeam-events --query TopicArn --output text)

# make_queue <name> <filter-policy-json>: queue + DLQ (3 receives) + filtered subscription.
make_queue() {
  local name=$1 filter=$2
  local dlq_url dlq_arn queue_url queue_arn attrs
  dlq_url=$(awslocal sqs create-queue --queue-name "${name}-dlq" --query QueueUrl --output text)
  dlq_arn=$(awslocal sqs get-queue-attributes --queue-url "$dlq_url" --attribute-names QueueArn \
    --query Attributes.QueueArn --output text)
  queue_url=$(awslocal sqs create-queue --queue-name "$name" --query QueueUrl --output text)
  attrs=$(mktemp)
  cat > "$attrs" <<JSON
{
  "VisibilityTimeout": "30",
  "RedrivePolicy": "{\"deadLetterTargetArn\":\"${dlq_arn}\",\"maxReceiveCount\":\"3\"}"
}
JSON
  awslocal sqs set-queue-attributes --queue-url "$queue_url" --attributes "file://$attrs"
  rm -f "$attrs"
  queue_arn=$(awslocal sqs get-queue-attributes --queue-url "$queue_url" --attribute-names QueueArn \
    --query Attributes.QueueArn --output text)
  awslocal sns subscribe --topic-arn "$topic_arn" --protocol sqs --notification-endpoint "$queue_arn" \
    --attributes "{\"RawMessageDelivery\":\"true\",\"FilterPolicy\":$(printf '%s' "$filter" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')}" \
    --query SubscriptionArn --output text
  echo "queue $queue_url (dlq $dlq_arn) filter $filter"
}

make_queue hookbeam-worker '{"type":[{"anything-but":["delivery.due"]}]}'
make_queue hookbeam-delivery-worker '{"type":["delivery.due"]}'

echo "hookbeam events ready: topic $topic_arn"
