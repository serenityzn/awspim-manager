package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"pim-manager/pkg/dynamodb"
	"pim-manager/pkg/identitycenter"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"
)

var (
	expiration    dynamodb.Timestamp = 60 // 1 minute
	Region        string
	DynamoDbTable string

	// Test SQS event for local testing
	testSQSEvent = events.SQSEvent{
		Records: []events.SQSMessage{
			{
				MessageId:     "19dd0b57-b21e-4ac1-bd88-01bbb068cb78",
				ReceiptHandle: "MessageReceiptHandle",
				Body:          "Hello from SQS!",
				Attributes: map[string]string{
					"ApproximateReceiveCount":          "1",
					"SentTimestamp":                    "1523232000000",
					"SenderId":                         "123456789012",
					"ApproximateFirstReceiveTimestamp": "1523232000001",
				},
				MessageAttributes: map[string]events.SQSMessageAttribute{},
				Md5OfBody:         "{{{md5_of_body}}}",
				EventSource:       "aws:sqs",
				EventSourceARN:    "arn:aws:sqs:us-east-1:123456789012:MyQueue",
				AWSRegion:         "us-east-1",
			},
		},
	}
)

func init() {
	Region = os.Getenv("AWS_REGION")
	if Region == "" {
		panic(fmt.Errorf("AWS_REGION is not set!"))
	}

	DynamoDbTable = os.Getenv("DYNAMO_TABLE")
	if DynamoDbTable == "" {
		panic(fmt.Errorf("DynamoDbTable is not set!"))
	}
}

// lambdaHandler handles SQS events from AWS Lambda
func lambdaHandler(ctx context.Context, sqsEvent events.SQSEvent) error {
	fmt.Printf("Received SQS event with %d records\n", len(sqsEvent.Records))
	
	for i, record := range sqsEvent.Records {
		fmt.Printf("=== SQS Record %d/%d ===\n", i+1, len(sqsEvent.Records))
		fmt.Printf("MessageId: %s\n", record.MessageId)
		fmt.Printf("EventSource: %s\n", record.EventSource)
		fmt.Printf("EventSourceARN: %s\n", record.EventSourceARN)
		fmt.Printf("ReceiptHandle: %s\n", record.ReceiptHandle)
		fmt.Printf("Body: %s\n", record.Body)
		
		// Pretty print the message body if it's JSON
		var prettyJSON map[string]interface{}
		if err := json.Unmarshal([]byte(record.Body), &prettyJSON); err == nil {
			prettyBytes, _ := json.MarshalIndent(prettyJSON, "", "  ")
			fmt.Printf("Parsed JSON Body:\n%s\n", string(prettyBytes))
		}
		
		// Print message attributes if any
		if len(record.MessageAttributes) > 0 {
			fmt.Println("Message Attributes:")
			for key, attr := range record.MessageAttributes {
				var value, dataType string
				if attr.StringValue != nil {
					value = *attr.StringValue
				}
				dataType = attr.DataType
				fmt.Printf("  %s: %s (Type: %s)\n", key, value, dataType)
			}
		}
		
		fmt.Printf("Attributes: %+v\n", record.Attributes)
		fmt.Println("========================")
	}
	
	return nil
}

func main() {
	// Check if running in Lambda environment
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		fmt.Println("Starting Lambda handler...")
		lambda.Start(lambdaHandler)
		return
	}

	// Local development/testing mode
	fmt.Println("Running in local mode...")
	
	// Test the Lambda handler with test SQS event
	fmt.Println("Testing Lambda handler with test SQS event...")
	err := lambdaHandler(context.Background(), testSQSEvent)
	if err != nil {
		fmt.Printf("Error in Lambda handler: %v\n", err)
		panic(err)
	}
	fmt.Println("Lambda handler test completed successfully!")
	
	// Run the existing expired sessions check
	err = sqsCheckExpiredSessions(DynamoDbTable, Region)
	if err != nil {
		fmt.Printf("Error checking expired sessions: %v\n", err)
		panic(err)
	}
	// request := dynamodb.DynamoDbPimRequests{
	// 	Requester: "test.sso@popai.health",
	// 	Approver:  "volodymyr.l@popai.health",
	// 	AccountID: "904924507160",
	// }

	// err := sqsAssignPermissionSet("pim-requests", request, "DataTeam", "us-east-2")
	// if err != nil {
	// 	fmt.Printf("Error assigning permission set via SQS: %v\n", err)

	// 	panic(err)
	// }

}

func sqsCheckExpiredSessions(table string, region string) error {
	err := updateExpiredSessions(table, region)
	if err != nil {
		fmt.Printf("Error updating expired sessions: %v\n", err)
		return err
	}

	return nil
}

func dynamoDbNewRequest(table string, item dynamodb.DynamoDbPimRequests) error {
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	fmt.Println("Writing item to DynamoDB...")

	err = dynamo.WriteItem(item)
	if err != nil {
		fmt.Printf("Error writing item: %v\n", err)
		return err
	}
	fmt.Println("Item written successfully!")

	return nil
}

func dynamoDbUpdateRequestStatus(table string, status dynamodb.RequestStatus, requestId string) error {
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	valueTimestamp, err := dynamo.GetCreatedTimestamp(requestId)
	if err != nil {
		fmt.Printf("Error getting created timestamp: %v\n", err)
		return err
	}
	fmt.Printf("Created timestamp retrieved successfully: %v\n", valueTimestamp)

	err = dynamo.UpdateItemStatus(requestId, valueTimestamp, status)
	if err != nil {
		fmt.Printf("Error updating item: %v\n", err)
		return err
	}
	fmt.Println("Item updated successfully!")

	return nil
}

func dynamoDbInitialize(table string) (*dynamodb.DynamoDbConfig, error) {
	var awsConfig dynamodb.AwsConfig
	awsConfig.Region = "us-east-2"

	fmt.Println("Creating DynamoDB config...")
	dynamo, err := dynamodb.NewDynamoDbConfig(awsConfig, table)
	if err != nil {
		fmt.Printf("Error creating DynamoDB config: %v\n", err)
		return nil, err
	}

	fmt.Println("Checking if table exists...")
	err = dynamo.CheckTableExists()
	if err != nil {
		fmt.Printf("Error checking table: %v\n", err)
		fmt.Println("The table 'pim-requests' might not exist in your AWS account")
		return nil, err
	}
	fmt.Println("Table exists!")

	return dynamo, nil
}

func updateExpiredSessions(table string, region string) error {
	var count int = 0
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	expiredRecords, err := dynamo.GetExpired(false)
	if err != nil {
		return err
	}
	fmt.Printf("Expired items [%v]: %v\n", len(expiredRecords), expiredRecords)

	for i := range expiredRecords {
		err := identityCenterRemovePermissionSetWithApprover(expiredRecords[i].Requester, expiredRecords[i].AccountID, "DataTeam", expiredRecords[i].Approver, region)
		if err != nil {
			fmt.Printf("Error removing permission set: %v\n", err)
			return err
		}

		err = dynamo.UpdateItemStatus(expiredRecords[i].RequestID, expiredRecords[i].CreatedTimestamp, dynamodb.Expired)
		if err != nil {
			fmt.Printf("Error updating item status to Expired: %v. Skiping...\n", err)
			return err
		}
		count++
		fmt.Printf("[%v of %v] Item with RequestID %s marked as Expired\n", count, len(expiredRecords), expiredRecords[i].RequestID)
	}

	return nil
}

func identityCenterInitialize(region string) (*identitycenter.IdentityCenterConfig, error) {
	ic, err := identitycenter.NewIdentityCenterConfig(region)
	if err != nil {
		fmt.Printf("Error creating Identity Center config: %v\n", err)
		return nil, err
	}

	return ic, nil
}

func identityCenterAssignPermissionSet(email, accountID, permissionSet string, region string) error {
	ic, err := identityCenterInitialize(region)
	if err != nil {
		return err
	}

	err = ic.AssignUserToAccountByEmail(email, accountID, permissionSet)
	if err != nil {
		fmt.Printf("Error assigning permission set: %v\n", err)
		return err
	}

	return nil
}

func validateApprover(approverEmail string, region string) error {
	ic, err := identityCenterInitialize(region)
	if err != nil {
		return err
	}

	fmt.Printf("Validating approver: %s...\n", approverEmail)
	_, err = ic.FindUserByEmail(approverEmail)
	if err != nil {
		return fmt.Errorf("approver validation failed: approver %s is not a valid user in the organization: %v", approverEmail, err)
	}

	fmt.Printf("✅ Approver %s validated successfully\n", approverEmail)
	return nil
}

func identityCenterRemovePermissionSet(email, accountID, permissionSet string, region string) error {
	ic, err := identityCenterInitialize(region)
	if err != nil {
		return err
	}

	err = ic.RemoveUserFromAccountByEmail(email, accountID, permissionSet)
	if err != nil {
		fmt.Printf("Error removing permission set: %v\n", err)
		return err
	}

	return nil
}

func identityCenterRemovePermissionSetWithApprover(email, accountID, permissionSet, approverEmail, region string) error {
	// Validate approver before proceeding with removal
	err := validateApprover(approverEmail, region)
	if err != nil {
		fmt.Printf("⚠️  Skipping permission removal: %v\n", err)
		return err
	}

	return identityCenterRemovePermissionSet(email, accountID, permissionSet, region)
}

func sqsAssignPermissionSet(table string, item dynamodb.DynamoDbPimRequests, permissionSet string, region string) error {
	err := validateApprover(item.Approver, region)
	if err != nil {
		fmt.Printf("⚠️  Skipping permission assignment: %v\n", err)
		return err
	}

	err = identityCenterAssignPermissionSet(item.Requester, item.AccountID, permissionSet, region)
	if err != nil {
		fmt.Printf("Error assigning permission set: %v\n", err)
		return err
	}

	var currentTime = dynamodb.Timestamp(time.Now().Unix())
	var requestID = uuid.New().String()

	item.CreatedTimestamp = currentTime
	item.RequestID = requestID
	item.ExpirationTimestamp = currentTime + expiration
	item.Status = dynamodb.Approved

	err = dynamoDbNewRequest(table, item)
	if err != nil {
		fmt.Printf("Error storing request in DynamoDB: %v\n", err)
		return err
	}

	return nil
}
