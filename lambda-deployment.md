# AWS Lambda Deployment Guide

## Lambda Handler Function

The `lambdaHandler` function in `main.go` is designed to process SQS events as AWS Lambda triggers.

### Function Features

- **Automatic Environment Detection**: Detects if running in Lambda vs local mode
- **SQS Event Processing**: Handles multiple SQS records per event
- **Message Parsing**: Pretty-prints JSON message bodies
- **Complete Logging**: Displays all message attributes and metadata

### Sample Output

```
Received SQS event with 2 records
=== SQS Record 1/2 ===
MessageId: 12345678-1234-1234-1234-123456789012
EventSource: aws:sqs
EventSourceARN: arn:aws:sqs:us-east-1:123456789012:my-queue
ReceiptHandle: AQEB...
Body: {"action":"assign","user":"john.doe@company.com","account":"123456789012"}
Parsed JSON Body:
{
  "action": "assign",
  "user": "john.doe@company.com", 
  "account": "123456789012"
}
Attributes: map[ApproximateReceiveCount:1 SentTimestamp:1234567890000]
========================
```

## Deployment Steps

### 1. Build for Lambda

```bash
# Build for Linux (Lambda runtime)
GOOS=linux GOARCH=amd64 go build -o bootstrap main.go

# Create deployment package
zip lambda-function.zip bootstrap
```

### 2. Create Lambda Function

```bash
# Using AWS CLI
aws lambda create-function \
    --function-name pim-sqs-processor \
    --runtime provided.al2 \
    --role arn:aws:iam::YOUR-ACCOUNT:role/lambda-execution-role \
    --handler bootstrap \
    --zip-file fileb://lambda-function.zip \
    --environment Variables='{
        "AWS_REGION":"us-east-1",
        "DYNAMO_TABLE":"pim-requests",
        "adminUsers":"admin@company.com,superuser@company.com"
    }'
```

### 3. Set up SQS Trigger

```bash
# Add SQS as event source
aws lambda create-event-source-mapping \
    --function-name pim-sqs-processor \
    --event-source-arn arn:aws:sqs:us-east-1:YOUR-ACCOUNT:your-queue-name \
    --batch-size 10
```

## IAM Permissions

### Lambda Execution Role

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
                "sso:*",
                "sso-admin:*",
                "identitystore:*",
                "organizations:DescribeAccount"
            ],
            "Resource": "*"
        }
    ]
}
```

## Local Testing

### Test with Sample SQS Event

Create `test-event.json`:

```json
{
    "Records": [
        {
            "messageId": "test-message-1",
            "receiptHandle": "test-receipt-handle",
            "body": "{\"action\":\"assign\",\"user\":\"test@example.com\",\"account\":\"123456789012\"}",
            "attributes": {
                "ApproximateReceiveCount": "1",
                "SentTimestamp": "1234567890000"
            },
            "messageAttributes": {},
            "eventSource": "aws:sqs",
            "eventSourceARN": "arn:aws:sqs:us-east-1:123456789012:test-queue"
        }
    ]
}
```

### Run Locally

```bash
# Set environment variables
export AWS_REGION=us-east-1
export DYNAMO_TABLE=pim-requests
export adminUsers="admin@company.com"

# Run in local mode (without Lambda environment)
go run main.go

# Or test with lambda-go-test (if installed)
# echo '{}' | lambda-go-test -handler lambdaHandler
```

## Environment Variables

| Variable | Description | Required |
|----------|-------------|----------|
| `AWS_REGION` | AWS region for services | Yes |
| `DYNAMO_TABLE` | DynamoDB table name | Yes |
| `adminUsers` | Comma-separated admin emails | No |
| `AWS_LAMBDA_FUNCTION_NAME` | Auto-set by Lambda runtime | Auto |

## Monitoring

- **CloudWatch Logs**: Automatic logging to `/aws/lambda/pim-sqs-processor`
- **CloudWatch Metrics**: Lambda duration, errors, invocations
- **X-Ray Tracing**: Enable for detailed request tracing

## Error Handling

The Lambda function currently prints all SQS messages and returns without errors. For production use, consider:

1. **Error Handling**: Add try-catch for message processing
2. **Dead Letter Queues**: Configure SQS DLQ for failed messages  
3. **Partial Batch Failures**: Return specific failed message IDs
4. **Retry Logic**: Implement exponential backoff for AWS API calls

## Scaling

- **Concurrent Executions**: Lambda auto-scales based on SQS queue length
- **Batch Size**: Adjust `--batch-size` parameter (1-10 for SQS)
- **Reserved Concurrency**: Set limits to control scaling
