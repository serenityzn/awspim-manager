# awspim-manager

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)

AWS Lambda service that manages temporary privileged access to AWS accounts using **AWS IAM Identity Center (SSO)**. It grants a user a permission set for a fixed duration, then automatically revokes it when the time expires.

This is the **backend (Lambda) component** of the PIM system. The **Slack bot frontend** that sends access requests lives at [github.com/serenityzn/awspim](https://github.com/serenityzn/awspim). The full **Terraform infrastructure** for the entire system is at [github.com/serenityzn/awspim/tree/main/terraform](https://github.com/serenityzn/awspim/tree/main/terraform).

---

## How it works

### Grant flow (SQS trigger)

```
Slack bot → SQS message → Lambda
                              ├── Validate approver (Secrets Manager allowlist)
                              ├── Block management account
                              ├── Assign permission set in Identity Center
                              ├── Write record + expiration to DynamoDB
                              └── Email requestor via SES
```

### Revoke flow (EventBridge scheduled trigger)

```
EventBridge schedule → Lambda
                           ├── Scan DynamoDB for expired records (status != Expired)
                           ├── Remove permission set from Identity Center
                           ├── Mark record as Expired in DynamoDB
                           └── Email requestor via SES
```

### SQS message format

The Slack bot sends messages in this format:

```json
{
  "request_id":             "a1b2c3d4e5f6g7h8",
  "requestor":              "user@example.com",
  "requestor_slack_user_id":"UXXXXXXXXX",
  "approver":               "manager@example.com",
  "account":                "123456789012",
  "datetime":               "2026-06-05 10:00"
}
```

After processing, the Lambda sends a result back to the response queue:

```json
{
  "request_id":   "a1b2c3d4e5f6g7h8",
  "slack_user_id":"UXXXXXXXXX",
  "account_id":   "123456789012",
  "account_name": "",
  "status":       "granted",
  "reason":       ""
}
```

| Status | Meaning |
|---|---|
| `granted` | Permission assigned successfully |
| `rejected` | Request refused (unauthorized approver, management account) |
| `failed` | Technical error during processing |
| `revoked` | Access removed by cleanup job |

---

## AWS services used

| Service | Purpose |
|---|---|
| **SQS** | Receives access requests from the Slack bot |
| **EventBridge** | Triggers the cleanup Lambda on a schedule |
| **IAM Identity Center** | Grants / revokes permission set assignments |
| **DynamoDB** | Tracks active and expired access requests |
| **Secrets Manager** | Stores the allowlist of authorized approvers |
| **SES** | Sends email notifications to requestors |

---

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `AWS_REGION` | Yes | — | AWS region for all service calls |
| `DYNAMO_TABLE` | Yes | — | DynamoDB table name (e.g. `pim-requests`) |
| `APPROVERS` | Yes | — | Secrets Manager secret name containing a JSON array of authorized approver emails |
| `MANAGEMENT_ACCOUNT` | Yes | — | AWS account ID of the management account (access to this account is always blocked) |
| `SQS_RESPONSE_QUEUE_URL` | Yes | — | Full URL of the Slack bot response queue (`PIM-SQS-Response`) |
| `SES_FROM_EMAIL` | Yes | — | Verified SES sender address for email notifications |
| `SESSION_TIMEOUT` | No | `3600` | Access duration in seconds |
| `PIM_ROLE` | No | `AdministratorAccess` | Name of the Identity Center permission set to grant/revoke |
| `LOG_LEVEL` | No | `info` | Log verbosity: `info` or `debug`. **Use `debug` only temporarily** — it logs PII (emails, account IDs) to CloudWatch |

### Approvers secret format

The secret referenced by `APPROVERS` must be a JSON array of email strings:

```json
["manager@example.com", "lead@example.com"]
```

---

## DynamoDB table

**Table name:** `pim-requests` (or whatever you set in `DYNAMO_TABLE`)

| Attribute | Type | Key |
|---|---|---|
| `request_id` | String | Partition key |
| `created_timestamp` | Number | Sort key |
| `expiration_timestamp` | Number | — |
| `requester` | String | — |
| `approver` | String | — |
| `account_id` | String | — |
| `status` | String | — (`Pending`, `Approved`, `Denied`, `Expired`) |

### Recommended GSI

Add a Global Secondary Index for efficient expiration queries (avoids full table scan):

| | Attribute |
|---|---|
| Index name | `status-expiration-index` |
| Partition key | `status` (String) |
| Sort key | `expiration_timestamp` (Number) |
| Projection | All |

### Recommended TTL

Enable TTL on `expiration_timestamp` so DynamoDB automatically removes old records ~48h after expiry.

---

## Build and deploy

### Prerequisites

- Go 1.24+
- Docker with `buildx`
- AWS CLI configured with sufficient permissions
- An ECR repository for the image

### 1. Build the binary

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o pim .
```

### 2. Build and push the Docker image

```bash
AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
AWS_REGION=us-east-2
IMAGE_URI=$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/awspim-manager:latest

aws ecr get-login-password --region $AWS_REGION \
  | docker login --username AWS --password-stdin \
    $AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com

docker buildx build \
  --platform linux/amd64 \
  --provenance=false \
  --push \
  -t $IMAGE_URI .
```

> **Apple Silicon (M1/M2/M3) users:** always pass `--platform linux/amd64` and `--provenance=false` to avoid Lambda image incompatibility errors.

### 3. Deploy to Lambda

**First deploy:**

```bash
aws lambda create-function \
  --function-name awspim-manager \
  --package-type Image \
  --code ImageUri=$IMAGE_URI \
  --role arn:aws:iam::$AWS_ACCOUNT_ID:role/your-lambda-execution-role \
  --timeout 300 \
  --memory-size 256 \
  --environment Variables='{
    "AWS_REGION":"us-east-2",
    "DYNAMO_TABLE":"pim-requests",
    "APPROVERS":"pim-approvers-secret",
    "MANAGEMENT_ACCOUNT":"123456789012",
    "SESSION_TIMEOUT":"3600",
    "PIM_ROLE":"AdministratorAccess",
    "LOG_LEVEL":"info"
  }'
```

**Update existing Lambda:**

```bash
aws lambda update-function-code \
  --function-name awspim-manager \
  --image-uri $IMAGE_URI
```

---

## IAM permissions required

The Lambda execution role needs:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "logs:CreateLogGroup",
        "logs:CreateLogStream",
        "logs:PutLogEvents"
      ],
      "Resource": "arn:aws:logs:*:*:*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes"
      ],
      "Resource": "arn:aws:sqs:*:*:your-queue-name"
    },
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:Query",
        "dynamodb:Scan"
      ],
      "Resource": "arn:aws:dynamodb:*:*:table/pim-requests"
    },
    {
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue"
      ],
      "Resource": "arn:aws:secretsmanager:*:*:secret:pim-approvers-secret*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "ses:SendEmail"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "sso:ListInstances",
        "sso:ListPermissionSets",
        "sso:DescribePermissionSet",
        "sso:CreateAccountAssignment",
        "sso:DeleteAccountAssignment",
        "sso:DescribeAccountAssignmentCreationStatus",
        "sso:DescribeAccountAssignmentDeletionStatus",
        "sso:ListAccountAssignments",
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

## Testing in the AWS Console

### Simulate a scheduled cleanup (EventBridge)

Use the **CloudWatch Events** template with this payload:

```json
{
  "version": "0",
  "id": "12345678-1234-1234-1234-123456789012",
  "source": "aws.events",
  "account": "123456789012",
  "time": "2026-03-11T10:00:00Z",
  "region": "us-east-2",
  "detail-type": "Scheduled Event",
  "detail": {}
}
```

### Simulate an access request (SQS)

Use the **SQS** template with this payload:

```json
{
  "Records": [
    {
      "messageId": "test-001",
      "receiptHandle": "test",
      "eventSource": "aws:sqs",
      "eventSourceARN": "arn:aws:sqs:us-east-2:123456789012:pim-queue",
      "body": "{\"requestor\":\"user@example.com\",\"approver\":\"manager@example.com\",\"account\":\"111122223333\",\"datetime\":\"2026-03-11T10:00:00Z\"}",
      "attributes": {},
      "messageAttributes": {}
    }
  ]
}
```

---

## Manually revoking access

To force-revoke access for a user, update their DynamoDB record status back to `Approved` with an expiration in the past, then trigger the cleanup Lambda:

```bash
aws dynamodb update-item \
  --table-name pim-requests \
  --region us-east-2 \
  --key '{
    "request_id":        {"S": "their-request-id"},
    "created_timestamp": {"N": "their-created-timestamp"}
  }' \
  --update-expression "SET #s = :approved, expiration_timestamp = :expiry" \
  --expression-attribute-names '{"#s":"status"}' \
  --expression-attribute-values '{
    ":approved": {"S": "Approved"},
    ":expiry":   {"N": "1"}
  }'
```

Then run the CloudWatch Events test above to trigger the revocation.

---

## Repository structure

```
awspim-manager/
├── main.go                        # Lambda entrypoint, SQS and EventBridge handlers
├── Dockerfile                     # Container image for Lambda
├── go.mod / go.sum
├── pkg/
│   ├── identitycenter/            # AWS Identity Center (SSO) client
│   └── dynamodb/                  # DynamoDB client and data model
└── lambda-deployment.md           # Additional deployment notes
```

## Related

- **Slack bot (frontend):** [github.com/serenityzn/awspim](https://github.com/serenityzn/awspim)
- **Terraform (full infrastructure):** [github.com/serenityzn/awspim/tree/main/terraform](https://github.com/serenityzn/awspim/tree/main/terraform)

## License

This project is licensed under the [GNU General Public License v3.0](LICENSE) — free to use, modify, and distribute, but all derivative works must also be open source and retain the original copyright.
