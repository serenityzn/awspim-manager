# Lambda Deployment Guide

## Overview

The Lambda function handles two event types:

| Event source | Purpose |
|---|---|
| SQS (`PIM-SQS` / `PIM-SQS-TEST`) | Processes Slack bot approval requests — assigns AWS SSO permissions and sends a result back to the response queue |
| EventBridge (scheduled rule) | Runs periodic cleanup — revokes expired temporary access |

---

## 1. Build

```bash
# Target the Lambda execution environment (arm64 = Graviton2, cheapest; swap for amd64 if needed)
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap .

# Package
zip lambda-function.zip bootstrap
```

---

## 2. Create the Lambda Function

```bash
aws lambda create-function \
  --function-name pim-manager \
  --runtime provided.al2023 \
  --architectures arm64 \
  --role arn:aws:iam::YOUR_ACCOUNT_ID:role/pim-manager-execution-role \
  --handler bootstrap \
  --zip-file fileb://lambda-function.zip \
  --timeout 60 \
  --memory-size 256 \
  --environment 'Variables={
    AWS_REGION=us-east-2,
    DYNAMO_TABLE=pim-requests,
    APPROVERS=pim-approvers-secret,
    MANAGEMENT_ACCOUNT=000000000000,
    SESSION_TIMEOUT=3600,
    PIM_ROLE=AdministratorAccess,
    SQS_RESPONSE_QUEUE_URL=https://sqs.us-east-2.amazonaws.com/SLACK_BOT_ACCOUNT_ID/PIM-SQS-Response,
    SES_FROM_EMAIL=noreply@yourdomain.com,
    LOG_LEVEL=info
  }'
```

> Replace `YOUR_ACCOUNT_ID`, `YOUR_MANAGEMENT_ACCOUNT_ID`, `SLACK_BOT_ACCOUNT_ID` (the AWS account that owns the Slack bot's response queue), and all other placeholder values with your real values.

### Update existing function code

```bash
aws lambda update-function-code \
  --function-name pim-manager \
  --zip-file fileb://lambda-function.zip
```

### Update environment variables

```bash
aws lambda update-function-configuration \
  --function-name pim-manager \
  --environment 'Variables={
    AWS_REGION=us-east-2,
    DYNAMO_TABLE=pim-requests,
    APPROVERS=pim-approvers-secret,
    MANAGEMENT_ACCOUNT=YOUR_MANAGEMENT_ACCOUNT_ID,
    SESSION_TIMEOUT=3600,
    PIM_ROLE=AdministratorAccess,
    SQS_RESPONSE_QUEUE_URL=https://sqs.us-east-2.amazonaws.com/SLACK_BOT_ACCOUNT_ID/PIM-SQS-Response,
    SES_FROM_EMAIL=noreply@yourdomain.com,
    LOG_LEVEL=info
  }'
```

---

## 3. Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `AWS_REGION` | **Yes** | — | AWS region for all service calls |
| `DYNAMO_TABLE` | **Yes** | — | DynamoDB table that stores active PIM sessions |
| `APPROVERS` | **Yes** | — | Secrets Manager secret name containing the allowed approver email list (JSON array) |
| `MANAGEMENT_ACCOUNT` | **Yes** | — | AWS account ID of the management account — access requests for this account are always rejected |
| `SQS_RESPONSE_QUEUE_URL` | **Yes** | — | Full URL of the Slack bot response queue (`PIM-SQS-Response`) |
| `SES_FROM_EMAIL` | **Yes** | — | Verified SES sender address used for email notifications (e.g. `noreply@yourdomain.com`) |
| `SESSION_TIMEOUT` | No | `3600` | How long (seconds) temporary access is valid before automatic revocation |
| `PIM_ROLE` | No | `AdministratorAccess` | Name or ARN of the SSO permission set to assign |
| `LOG_LEVEL` | No | `info` | Verbosity: `info` or `debug` (debug logs PII — do not use in production) |

### Approvers secret format

The value stored in Secrets Manager under `APPROVERS` must be a JSON array of email addresses:

```json
["manager@example.com", "admin@example.com"]
```

---

## 4. SQS Trigger (Request Queue)

```bash
# Test environment
aws lambda create-event-source-mapping \
  --function-name pim-manager \
  --event-source-arn arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS-TEST \
  --batch-size 1 \
  --maximum-batching-window-in-seconds 0

# Production environment
aws lambda create-event-source-mapping \
  --function-name pim-manager \
  --event-source-arn arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS \
  --batch-size 1 \
  --maximum-batching-window-in-seconds 0
```

> `batch-size 1` is recommended so each approval is processed and responded to individually.

---

## 5. EventBridge Trigger (Scheduled Cleanup)

```bash
# Create a rule that fires every 15 minutes
aws events put-rule \
  --name pim-cleanup-schedule \
  --schedule-expression "rate(15 minutes)" \
  --state ENABLED

# Allow EventBridge to invoke the Lambda
aws lambda add-permission \
  --function-name pim-manager \
  --statement-id pim-cleanup-schedule \
  --action lambda:InvokeFunction \
  --principal events.amazonaws.com \
  --source-arn arn:aws:events:us-east-2:YOUR_ACCOUNT_ID:rule/pim-cleanup-schedule

# Attach Lambda as the target
aws events put-targets \
  --rule pim-cleanup-schedule \
  --targets "Id=pim-manager,Arn=arn:aws:lambda:us-east-2:YOUR_ACCOUNT_ID:function:pim-manager"
```

---

## 6. IAM Execution Role

Create role `pim-manager-execution-role` with trust policy for Lambda:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Service": "lambda.amazonaws.com" },
    "Action": "sts:AssumeRole"
  }]
}
```

Attach the following inline permission policy:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "CloudWatchLogs",
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:*:*:*"
    },
    {
      "Sid": "SQSRequestQueue",
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": [
        "arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS",
        "arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS-TEST"
      ]
    },
    {
      "Sid": "SQSResponseQueue",
      "Effect": "Allow",
      "Action": "sqs:SendMessage",
      "Resource": "arn:aws:sqs:us-east-2:SLACK_BOT_ACCOUNT_ID:PIM-SQS-Response"
    },
    {
      "Sid": "DynamoDB",
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:Query",
        "dynamodb:Scan",
        "dynamodb:DescribeTable"
      ],
      "Resource": "arn:aws:dynamodb:us-east-2:YOUR_ACCOUNT_ID:table/pim-requests"
    },
    {
      "Sid": "SecretsManager",
      "Effect": "Allow",
      "Action": "secretsmanager:GetSecretValue",
      "Resource": "arn:aws:secretsmanager:us-east-2:YOUR_ACCOUNT_ID:secret:pim-approvers-secret*"
    },
    {
      "Sid": "SES",
      "Effect": "Allow",
      "Action": "ses:SendEmail",
      "Resource": "*"
    },
    {
      "Sid": "IdentityCenter",
      "Effect": "Allow",
      "Action": [
        "sso-admin:ListInstances",
        "sso-admin:ListPermissionSets",
        "sso-admin:CreateAccountAssignment",
        "sso-admin:DeleteAccountAssignment",
        "sso-admin:ListAccountAssignments",
        "sso-admin:DescribePermissionSet",
        "identitystore:ListUsers",
        "identitystore:DescribeUser",
        "organizations:DescribeAccount"
      ],
      "Resource": "*"
    }
  ]
}
```

---

## 7. SQS Message Formats

### Incoming request (from Slack bot → `PIM-SQS` / `PIM-SQS-TEST`)

```json
{
  "request_id": "a1b2c3d4e5f6g7h8",
  "requestor": "user@example.com",
  "requestor_slack_user_id": "U0984U1QKFY",
  "approver": "manager@example.com",
  "account": "123456789012",
  "datetime": "2026-06-05 10:00"
}
```

### Outgoing response (Lambda → `PIM-SQS-Response`)

```json
{
  "request_id": "a1b2c3d4e5f6g7h8",
  "slack_user_id": "U0984U1QKFY",
  "account_id": "123456789012",
  "account_name": "",
  "status": "granted",
  "reason": ""
}
```

| Status | Meaning |
|---|---|
| `granted` | Permission assigned successfully |
| `rejected` | Request refused (unauthorized approver, management account, etc.) |
| `failed` | Technical error during assignment |
| `revoked` | Access removed (used by cleanup path, not this handler) |

---

## 8. Test Locally

### Run unit tests

```bash
go test -v ./...
```

### Invoke the Lambda directly via AWS CLI

```bash
# Create a test payload
cat > /tmp/test-event.json << 'EOF'
{
  "Records": [
    {
      "messageId": "test-message-1",
      "receiptHandle": "test-receipt-handle",
      "body": "{\"request_id\":\"a1b2c3d4e5f6\",\"requestor\":\"user@example.com\",\"requestor_slack_user_id\":\"U0984U1QKFY\",\"approver\":\"manager@example.com\",\"account\":\"123456789012\",\"datetime\":\"2026-06-05 10:00\"}",
      "attributes": {
        "ApproximateReceiveCount": "1",
        "SentTimestamp": "1234567890000"
      },
      "messageAttributes": {},
      "eventSource": "aws:sqs",
      "eventSourceARN": "arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS-TEST"
    }
  ]
}
EOF

aws lambda invoke \
  --function-name pim-manager \
  --payload file:///tmp/test-event.json \
  --cli-binary-format raw-in-base64-out \
  /tmp/response.json && cat /tmp/response.json
```

### Manually trigger the cleanup (EventBridge-style event)

```bash
cat > /tmp/cleanup-event.json << 'EOF'
{
  "version": "0",
  "id": "test-event-id",
  "source": "aws.events",
  "detail-type": "Scheduled Event",
  "time": "2026-06-05T10:00:00Z",
  "region": "us-east-2",
  "detail": {}
}
EOF

aws lambda invoke \
  --function-name pim-manager \
  --payload file:///tmp/cleanup-event.json \
  --cli-binary-format raw-in-base64-out \
  /tmp/response.json && cat /tmp/response.json
```

---

## 9. Monitoring

| What | Where |
|---|---|
| Execution logs | CloudWatch Logs → `/aws/lambda/pim-manager` |
| Error rate | CloudWatch Metrics → `AWS/Lambda` → `Errors` |
| SQS queue depth | CloudWatch Metrics → `AWS/SQS` → `ApproximateNumberOfMessagesVisible` |
| Dead-letter queue | Configure a DLQ on `PIM-SQS` to catch messages that fail repeatedly |

Enable `LOG_LEVEL=debug` temporarily to see full message bodies and email addresses in logs. **Do not leave debug logging on in production** — it logs PII.

---

## 10. Dead-Letter Queue (recommended)

Configure a DLQ on the request queue so that messages that fail processing 3+ times are captured for manual inspection:

```bash
aws sqs set-queue-attributes \
  --queue-url https://sqs.us-east-2.amazonaws.com/YOUR_ACCOUNT_ID/PIM-SQS \
  --attributes '{"RedrivePolicy":"{\"deadLetterTargetArn\":\"arn:aws:sqs:us-east-2:YOUR_ACCOUNT_ID:PIM-SQS-DLQ\",\"maxReceiveCount\":\"3\"}"}'
```
