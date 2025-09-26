# DynamoDB Package

This package provides a wrapper for AWS DynamoDB operations specifically designed for a Privileged Identity Management (PIM) system for AWS temporary credentials.

## Overview

The DynamoDB package manages PIM requests that track user access requests, approvals, and expiration times. It provides methods for creating, reading, updating, and querying PIM request records.

## Data Structure

### PIM Request Record

Each PIM request contains the following fields:

```go
type DynamoDbPimRequests struct {
    RequestID           string        // Unique identifier for the request
    CreatedTimestamp    Timestamp     // When the request was created (Unix timestamp)
    ExpirationTimestamp Timestamp     // When the access expires (Unix timestamp)
    Requester           string        // Username of the person requesting access
    Approver            string        // Username of the person who approved/will approve
    AccountID           string        // AWS Account ID for the access request
    Status              RequestStatus // Current status of the request
}
```

### Request Status Enum

```go
type RequestStatus int

const (
    Pending RequestStatus = iota + 1  // Request awaiting approval
    Approved                          // Request has been approved
    Denied                           // Request has been denied
    Expired                          // Request has expired
)
```

### Custom Timestamp Type

```go
type Timestamp int64

func (t Timestamp) String() string {
    return strconv.FormatInt(int64(t), 10)
}
```

## DynamoDB Table Schema

### Primary Key Structure
- **Partition Key**: `request_id` (String)
- **Sort Key**: `created_timestamp` (Number)

This composite key allows for:
- Unique identification of requests
- Time-based sorting and querying
- Efficient retrieval of specific requests

### Sample Table Item
```json
{
    "request_id": "a7eef693-b154-4f10-9456-98cc363e9c0d",
    "created_timestamp": 1617181920,
    "expiration_timestamp": 1617185520,
    "requester": "john.doe",
    "approver": "admin",
    "account_id": "123456789012",
    "status": "Pending"
}
```

## Configuration

### AWS Configuration

```go
type AwsConfig struct {
    Region string  // AWS region for DynamoDB operations
}
```

### DynamoDB Configuration

```go
type DynamoDbConfig struct {
    sess      *dynamodb.DynamoDB  // DynamoDB session
    tableName string              // Table name for PIM requests
}
```

## Usage

### 1. Initialize Configuration

```go
import "pim-manager/pkg/dynamodb"

// Configure AWS settings
awsConfig := dynamodb.AwsConfig{
    Region: "us-east-2",
}

// Create DynamoDB configuration
dynamo, err := dynamodb.NewDynamoDbConfig(awsConfig, "pim-requests")
if err != nil {
    log.Fatal(err)
}
```

### 2. Check Table Existence

```go
// Verify the DynamoDB table exists
err := dynamo.CheckTableExists()
if err != nil {
    log.Printf("Table validation failed: %v", err)
    // Handle table creation or configuration
}
```

### 3. Create New PIM Request

```go
import (
    "time"
    "github.com/google/uuid"
)

// Create a new PIM request
currentTime := dynamodb.Timestamp(time.Now().Unix())
expirationTime := currentTime + 3600 // 1 hour from now

request := dynamodb.DynamoDbPimRequests{
    RequestID:           uuid.New().String(),
    CreatedTimestamp:    currentTime,
    ExpirationTimestamp: expirationTime,
    Requester:           "john.doe",
    Approver:            "admin",
    AccountID:           "123456789012",
    Status:              dynamodb.Pending,
}

err := dynamo.WriteItem(request)
if err != nil {
    log.Printf("Failed to create request: %v", err)
}
```

### 4. Update Request Status

```go
// Update request status (requires both partition and sort key)
err := dynamo.UpdateItemStatus(requestID, createdTimestamp, dynamodb.Approved)
if err != nil {
    log.Printf("Failed to update status: %v", err)
}
```

The `UpdateItemStatus` method includes smart validation:
- Checks current status before updating
- Skips update if status is already set to desired value
- Provides detailed logging of status changes

### 5. Get Creation Timestamp

```go
// Retrieve the creation timestamp for a request ID
timestamp, err := dynamo.GetCreatedTimestamp(requestID)
if err != nil {
    log.Printf("Failed to get timestamp: %v", err)
} else {
    log.Printf("Request created at: %d", timestamp)
}
```

### 6. Find Expired Requests

```go
// Get expired requests that need status update
expiredRequests, err := dynamo.GetExpired(false)
if err != nil {
    log.Printf("Failed to get expired requests: %v", err)
} else {
    log.Printf("Found %d expired requests needing update", len(expiredRequests))
}

// Get all expired requests regardless of status
allExpired, err := dynamo.GetExpired(true)
if err != nil {
    log.Printf("Failed to get all expired requests: %v", err)
} else {
    log.Printf("Total expired requests: %d", len(allExpired))
}
```

## Methods Reference

### Core Operations

#### `NewDynamoDbConfig(awsConfig AwsConfig, tableName string) (*DynamoDbConfig, error)`
Creates a new DynamoDB configuration with AWS session and table name.

#### `CheckTableExists() error`
Validates that the specified DynamoDB table exists and is accessible.

#### `WriteItem(item DynamoDbPimRequests) error`
Creates a new PIM request record in DynamoDB.

#### `UpdateItemStatus(requestID string, createdTimestamp Timestamp, status RequestStatus) error`
Updates the status of an existing PIM request. Includes validation to prevent unnecessary updates.

#### `GetCreatedTimestamp(requestID string) (Timestamp, error)`
Retrieves the creation timestamp for a given request ID by querying the table.

#### `GetExpired(allrecords bool) ([]DynamoDbPimRequests, error)`
Finds expired PIM requests based on expiration timestamp.
- `allrecords = true`: Returns all expired records regardless of status
- `allrecords = false`: Returns only expired records that don't have "Expired" status

### Getter Methods

#### `GetSession() *dynamodb.DynamoDB`
Returns the DynamoDB session for advanced operations.

#### `GetTableName() string`
Returns the configured table name.

## Error Handling

All methods include comprehensive error handling:

- **Connection errors**: AWS session and credential issues
- **Table validation**: Missing or inaccessible tables
- **Item conflicts**: Duplicate keys or constraint violations
- **Timeout protection**: 10-second default timeouts for operations
- **Validation errors**: Invalid request data or missing required fields

## Performance Considerations

### Efficient Querying
- Uses Query operations for request ID lookups (efficient)
- Uses Scan operations with filters for expired record searches (less efficient but necessary)
- Includes pagination support for large result sets

### Timeout Management
- Default 10-second timeouts for most operations
- Extended 30-second timeout for scan operations
- Context-based cancellation for all AWS operations

### Connection Reuse
- Single DynamoDB session per configuration instance
- Automatic credential management through AWS SDK
- Shared config state for credential file and environment variable support

## Best Practices

### 1. Request ID Generation
```go
// Use UUID for unique request IDs
requestID := uuid.New().String()
```

### 2. Timestamp Management
```go
// Use Unix timestamps for consistency
currentTime := dynamodb.Timestamp(time.Now().Unix())
expirationTime := currentTime + durationInSeconds
```

### 3. Status Updates
```go
// Always use the enum values for status
status := dynamodb.Approved  // Not "Approved" string
```

### 4. Error Handling
```go
if err != nil {
    log.Printf("Operation failed: %v", err)
    // Implement appropriate retry or fallback logic
}
```

### 5. Cleanup Operations
```go
// Regularly process expired requests
expiredRequests, err := dynamo.GetExpired(false)
if err == nil {
    for _, request := range expiredRequests {
        err := dynamo.UpdateItemStatus(request.RequestID, request.CreatedTimestamp, dynamodb.Expired)
        if err != nil {
            log.Printf("Failed to mark request %s as expired: %v", request.RequestID, err)
        }
    }
}
```

## Example: Complete PIM Workflow

```go
package main

import (
    "fmt"
    "log"
    "time"
    "pim-manager/pkg/dynamodb"
    "github.com/google/uuid"
)

func main() {
    // 1. Initialize DynamoDB
    awsConfig := dynamodb.AwsConfig{Region: "us-east-2"}
    dynamo, err := dynamodb.NewDynamoDbConfig(awsConfig, "pim-requests")
    if err != nil {
        log.Fatal(err)
    }

    // 2. Verify table exists
    if err := dynamo.CheckTableExists(); err != nil {
        log.Fatal(err)
    }

    // 3. Create new request
    currentTime := dynamodb.Timestamp(time.Now().Unix())
    request := dynamodb.DynamoDbPimRequests{
        RequestID:           uuid.New().String(),
        CreatedTimestamp:    currentTime,
        ExpirationTimestamp: currentTime + 3600, // 1 hour
        Requester:           "john.doe",
        Approver:            "admin",
        AccountID:           "123456789012",
        Status:              dynamodb.Pending,
    }

    if err := dynamo.WriteItem(request); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Created request: %s\n", request.RequestID)

    // 4. Approve request
    err = dynamo.UpdateItemStatus(request.RequestID, request.CreatedTimestamp, dynamodb.Approved)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Request approved")

    // 5. Process expired requests
    expiredRequests, err := dynamo.GetExpired(false)
    if err != nil {
        log.Fatal(err)
    }
    
    for _, expiredRequest := range expiredRequests {
        err := dynamo.UpdateItemStatus(expiredRequest.RequestID, expiredRequest.CreatedTimestamp, dynamodb.Expired)
        if err != nil {
            log.Printf("Failed to expire request %s: %v", expiredRequest.RequestID, err)
        } else {
            fmt.Printf("Expired request: %s\n", expiredRequest.RequestID)
        }
    }
}
```

## Dependencies

- AWS SDK for Go v1: `github.com/aws/aws-sdk-go`
- UUID generation: `github.com/google/uuid`

## Configuration Requirements

- AWS credentials configured (via environment variables, credentials file, or IAM role)
- DynamoDB table with the correct schema (composite primary key)
- Appropriate IAM permissions for DynamoDB operations

## IAM Permissions Required

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "dynamodb:PutItem",
                "dynamodb:GetItem",
                "dynamodb:UpdateItem",
                "dynamodb:Query",
                "dynamodb:Scan",
                "dynamodb:DescribeTable"
            ],
            "Resource": "arn:aws:dynamodb:*:*:table/pim-requests"
        }
    ]
}
```
