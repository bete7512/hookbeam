#!/usr/bin/env bash
# Creates the SNS topic and SQS queues the api and worker use locally. LocalStack runs it from
# /etc/localstack/init/ready.d once its services are up; every step is safe to repeat.
set -euo pipefail

topic_arn=$(awslocal sns create-topic --name hookbeam-events --query TopicArn --output text)

dlq_url=$(awslocal sqs create-queue --queue-name hookbeam-worker-dlq --query QueueUrl --output text)
dlq_arn=$(awslocal sqs get-queue-attributes --queue-url "$dlq_url" --attribute-names QueueArn \
  --query Attributes.QueueArn --output text)

queue_url=$(awslocal sqs create-queue --queue-name hookbeam-worker --query QueueUrl --output text)
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
  --attributes RawMessageDelivery=true --query SubscriptionArn --output text

echo "hookbeam events ready: topic $topic_arn -> queue $queue_url (dlq $dlq_arn)"
