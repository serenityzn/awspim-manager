package dynamodb

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
)

type RequestStatus int

const (
	Pending RequestStatus = iota + 1
	Approved
	Denied
	Expired
)

func (rs RequestStatus) String() string {
	switch rs {
	case Pending:
		return "Pending"
	case Approved:
		return "Approved"
	case Denied:
		return "Denied"
	case Expired:
		return "Expired"
	default:
		return "Unknown"
	}
}

func (rs RequestStatus) EnumIndex() int {
	return int(rs)
}

func ParseRequestStatus(status string) RequestStatus {
	switch status {
	case "Pending":
		return Pending
	case "Approved":
		return Approved
	case "Denied":
		return Denied
	case "Expired":
		return Expired
	default:
		return Pending
	}
}

type AwsConfig struct {
	Region string
}

type DynamoDbConfig struct {
	sess      *dynamodb.DynamoDB
	tableName string
}

// Custom type for timestamp that implements String() method
type Timestamp int64

// String method for Timestamp type
func (t Timestamp) String() string {
	return strconv.FormatInt(int64(t), 10)
}

type DynamoDbPimRequests struct {
	RequestID           string
	CreatedTimestamp    Timestamp
	ExpirationTimestamp Timestamp
	Requester           string
	Approver            string
	AccountID           string
	Status              RequestStatus
}

func NewDynamoDbConfig(awsConfig AwsConfig, tableName string) (*DynamoDbConfig, error) {
	session, err := startSession(awsConfig)
	if err != nil {
		return nil, err
	}

	return &DynamoDbConfig{
		sess:      session,
		tableName: tableName,
	}, nil
}

func (d *DynamoDbConfig) GetSession() *dynamodb.DynamoDB {
	return d.sess
}

func (d *DynamoDbConfig) GetTableName() string {
	return d.tableName
}

func init() {
}

func startSession(awsConfig AwsConfig) (*dynamodb.DynamoDB, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(awsConfig.Region),
			HTTPClient: &http.Client{
				Timeout: 10 * time.Second,
			},
		},
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, err
	}

	svc := dynamodb.New(sess)

	return svc, nil
}

func (db *DynamoDbConfig) CheckTableExists() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &dynamodb.DescribeTableInput{
		TableName: &db.tableName,
	}

	_, err := db.sess.DescribeTableWithContext(ctx, input)
	return err
}

func (db *DynamoDbConfig) WriteItem(item DynamoDbPimRequests) error {
	input := &dynamodb.PutItemInput{
		TableName: &db.tableName,
		Item: map[string]*dynamodb.AttributeValue{
			"request_id": {
				S: aws.String(item.RequestID),
			},
			"created_timestamp": {
				N: aws.String(item.CreatedTimestamp.String()),
			},
			"expiration_timestamp": {
				N: aws.String(item.ExpirationTimestamp.String()),
			},
			"requester": {
				S: aws.String(item.Requester),
			},
			"approver": {
				S: aws.String(item.Approver),
			},
			"account_id": {
				S: aws.String(item.AccountID),
			},
			"status": {
				S: aws.String(item.Status.String()),
			},
		},
	}

	// Add timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.sess.PutItemWithContext(ctx, input)
	if err != nil {
		return err
	}

	return nil
}

func (db *DynamoDbConfig) UpdateItemStatus(requestID string, createdTimestamp Timestamp, status RequestStatus) error {
	// First, get the current item to check its status
	getInput := &dynamodb.GetItemInput{
		TableName: &db.tableName,
		Key: map[string]*dynamodb.AttributeValue{
			"request_id": {
				S: aws.String(requestID),
			},
			"created_timestamp": {
				N: aws.String(createdTimestamp.String()),
			},
		},
		ProjectionExpression: aws.String("#status"),
		ExpressionAttributeNames: map[string]*string{
			"#status": aws.String("status"),
		},
	}

	// Add timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	getResult, err := db.sess.GetItemWithContext(ctx, getInput)
	if err != nil {
		return fmt.Errorf("failed to get current item: %v", err)
	}

	// Check if item exists
	if getResult.Item == nil {
		return fmt.Errorf("item not found with request_id: %s and timestamp: %d", requestID, createdTimestamp)
	}

	// Check current status
	currentStatusAttr := getResult.Item["status"]
	if currentStatusAttr == nil || currentStatusAttr.S == nil {
		return fmt.Errorf("status field not found for item with request_id: %s", requestID)
	}

	currentStatus := *currentStatusAttr.S

	// Skip update if status is already the desired status
	if currentStatus == status.String() {
		fmt.Printf("Status is already '%s' for request_id: %s, skipping update\n", status, requestID)
		return nil
	}

	// Proceed with update since status is different
	updateInput := &dynamodb.UpdateItemInput{
		TableName: &db.tableName,
		Key: map[string]*dynamodb.AttributeValue{
			"request_id": {
				S: aws.String(requestID),
			},
			"created_timestamp": {
				N: aws.String(createdTimestamp.String()),
			},
		},
		UpdateExpression: aws.String("SET #status = :status"),
		ExpressionAttributeNames: map[string]*string{
			"#status": aws.String("status"),
		},
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":status": {
				S: aws.String(status.String()),
			},
		},
		ReturnValues: aws.String("UPDATED_NEW"),
	}

	// Create new context for update operation
	updateCtx, updateCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer updateCancel()

	_, err = db.sess.UpdateItemWithContext(updateCtx, updateInput)
	if err != nil {
		return fmt.Errorf("failed to update status from '%s' to '%s': %v", currentStatus, status, err)
	}

	fmt.Printf("Successfully updated status from '%s' to '%s' for request_id: %s\n", currentStatus, status, requestID)
	return nil
}

func (db *DynamoDbConfig) GetCreatedTimestamp(requestID string) (Timestamp, error) {
	// Query all items with the given request_id to find the created_timestamp
	input := &dynamodb.QueryInput{
		TableName:              &db.tableName,
		KeyConditionExpression: aws.String("request_id = :requestid"),
		ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
			":requestid": {
				S: aws.String(requestID),
			},
		},
		ProjectionExpression: aws.String("created_timestamp"),
		Limit:                aws.Int64(1), // We only need one result
	}

	// Add timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := db.sess.QueryWithContext(ctx, input)
	if err != nil {
		return 0, err
	}

	if len(result.Items) == 0 {
		return 0, fmt.Errorf("no item found with request_id: %s", requestID)
	}

	// Extract the created_timestamp from the first (and only) item
	timestampAttr := result.Items[0]["created_timestamp"]
	if timestampAttr == nil || timestampAttr.N == nil {
		return 0, fmt.Errorf("created_timestamp not found for request_id: %s", requestID)
	}

	// Convert string back to int64
	timestampInt, err := strconv.ParseInt(*timestampAttr.N, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse timestamp: %v", err)
	}

	return Timestamp(timestampInt), nil
}

func (db *DynamoDbConfig) GetExpired(allrecords bool) ([]DynamoDbPimRequests, error) {
	currentTime := Timestamp(time.Now().Unix())

	var input *dynamodb.ScanInput

	if allrecords {
		// Return all expired records regardless of status
		input = &dynamodb.ScanInput{
			TableName:        &db.tableName,
			FilterExpression: aws.String("expiration_timestamp < :currentTime"),
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":currentTime": {
					N: aws.String(currentTime.String()),
				},
			},
		}
	} else {
		// Return only expired records that don't have status "Expired"
		input = &dynamodb.ScanInput{
			TableName:        &db.tableName,
			FilterExpression: aws.String("expiration_timestamp < :currentTime AND #status <> :expiredStatus"),
			ExpressionAttributeNames: map[string]*string{
				"#status": aws.String("status"),
			},
			ExpressionAttributeValues: map[string]*dynamodb.AttributeValue{
				":currentTime": {
					N: aws.String(currentTime.String()),
				},
				":expiredStatus": {
					S: aws.String(Expired.String()),
				},
			},
		}
	}

	// Add timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var allExpiredSessions []DynamoDbPimRequests

	// Use Scan with pagination to get all expired sessions
	err := db.sess.ScanPagesWithContext(ctx, input, func(page *dynamodb.ScanOutput, lastPage bool) bool {
		for _, item := range page.Items {
			session := DynamoDbPimRequests{}

			// Parse request_id
			if requestID := item["request_id"]; requestID != nil && requestID.S != nil {
				session.RequestID = *requestID.S
			}

			// Parse created_timestamp
			if createdTimestamp := item["created_timestamp"]; createdTimestamp != nil && createdTimestamp.N != nil {
				if ts, err := strconv.ParseInt(*createdTimestamp.N, 10, 64); err == nil {
					session.CreatedTimestamp = Timestamp(ts)
				}
			}

			// Parse expiration_timestamp
			if expirationTimestamp := item["expiration_timestamp"]; expirationTimestamp != nil && expirationTimestamp.N != nil {
				if ts, err := strconv.ParseInt(*expirationTimestamp.N, 10, 64); err == nil {
					session.ExpirationTimestamp = Timestamp(ts)
				}
			}

			// Parse requester
			if requester := item["requester"]; requester != nil && requester.S != nil {
				session.Requester = *requester.S
			}

			// Parse approver
			if approver := item["approver"]; approver != nil && approver.S != nil {
				session.Approver = *approver.S
			}

			// Parse account_id
			if accountID := item["account_id"]; accountID != nil && accountID.S != nil {
				session.AccountID = *accountID.S
			}

			// Parse status
			if status := item["status"]; status != nil && status.S != nil {
				session.Status = ParseRequestStatus(*status.S)
			}

			allExpiredSessions = append(allExpiredSessions, session)
		}

		// Continue to next page if not the last page
		return !lastPage
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan for expired records: %v", err)
	}

	return allExpiredSessions, nil
}
