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
)

func init() {
	fmt.Println("=== Lambda Function Initialization ===")

	Region = os.Getenv("AWS_REGION")
	fmt.Printf("AWS_REGION environment variable: '%s'\n", Region)
	if Region == "" {
		fmt.Println("ERROR: AWS_REGION is not set!")
		panic(fmt.Errorf("AWS_REGION is not set!"))
	}
	fmt.Printf("✅ AWS_REGION set to: %s\n", Region)

	DynamoDbTable = os.Getenv("DYNAMO_TABLE")
	fmt.Printf("DYNAMO_TABLE environment variable: '%s'\n", DynamoDbTable)
	if DynamoDbTable == "" {
		fmt.Println("ERROR: DynamoDbTable is not set!")
		panic(fmt.Errorf("DynamoDbTable is not set!"))
	}
	fmt.Printf("✅ DYNAMO_TABLE set to: %s\n", DynamoDbTable)

	fmt.Println("=== Lambda Function Initialization Complete ===")
}

// SQSMessage represents the expected structure of SQS message body
type SQSMessage struct {
	Requestor string `json:"requestor"`
	Approver  string `json:"approver"`
	Account   string `json:"account"`
	Datetime  string `json:"datetime"`
}

// lambdaHandler handles SQS events from AWS Lambda
func lambdaHandler(ctx context.Context, sqsEvent events.SQSEvent) error {
	fmt.Printf("🔔 Received SQS event with %d records\n", len(sqsEvent.Records))

	for i, record := range sqsEvent.Records {
		fmt.Printf("\n=== Processing SQS Record %d/%d ===\n", i+1, len(sqsEvent.Records))
		fmt.Printf("📝 MessageId: %s\n", record.MessageId)
		fmt.Printf("📋 EventSource: %s\n", record.EventSource)
		fmt.Printf("🔗 EventSourceARN: %s\n", record.EventSourceARN)
		fmt.Printf("📄 Raw Body: %s\n", record.Body)

		// Parse the SQS message body
		var message SQSMessage
		if err := json.Unmarshal([]byte(record.Body), &message); err != nil {
			fmt.Printf("❌ Error parsing SQS message body: %v\n", err)
			fmt.Printf("📄 Raw body that failed to parse: %s\n", record.Body)
			continue
		}

		// Log the extracted information
		fmt.Printf("\n🎯 === ACCESS REQUEST DETAILS ===\n")
		fmt.Printf("👤 Requestor: %s\n", message.Requestor)
		fmt.Printf("✅ Approver: %s\n", message.Approver)
		fmt.Printf("🏦 AWS Account: %s\n", message.Account)
		fmt.Printf("⏰ Request Time: %s\n", message.Datetime)
		fmt.Printf("🔐 Action: Grant Administration access to account %s for user %s (approved by %s)\n", 
			message.Account, message.Requestor, message.Approver)
		
		// Pretty print the entire message for debugging
		prettyBytes, _ := json.MarshalIndent(message, "", "  ")
		fmt.Printf("\n📊 Structured Message Data:\n%s\n", string(prettyBytes))
		
		// Process the access request
		fmt.Printf("\n🚀 === PROCESSING ACCESS REQUEST ===\n")
		
		// Create DynamoDB request object
		pimRequest := dynamodb.DynamoDbPimRequests{
			Requester: message.Requestor,
			Approver:  message.Approver,
			AccountID: message.Account,
		}
		
		// Assign Administration permission set
		permissionSet := "AdministratorAccess"
		fmt.Printf("🔑 Assigning permission set '%s' to user '%s' for account '%s'\n", 
			permissionSet, message.Requestor, message.Account)
		
		err := sqsAssignPermissionSet(DynamoDbTable, pimRequest, permissionSet, Region)
		if err != nil {
			fmt.Printf("❌ Error assigning permission set: %v\n", err)
			// Continue processing other messages instead of failing completely
			continue
		}
		
		fmt.Printf("✅ Successfully assigned Administration access to %s for account %s\n", 
			message.Requestor, message.Account)
		
		// Print message attributes if any
		if len(record.MessageAttributes) > 0 {
			fmt.Println("\n📎 Message Attributes:")
			for key, attr := range record.MessageAttributes {
				var value, dataType string
				if attr.StringValue != nil {
					value = *attr.StringValue
				}
				dataType = attr.DataType
				fmt.Printf("  %s: %s (Type: %s)\n", key, value, dataType)
			}
		}
		
		fmt.Println("========================")
	}

	fmt.Printf("✅ Successfully processed %d SQS records\n", len(sqsEvent.Records))
	return nil
}

func main() {
	fmt.Println("=== Main Function Started ===")

	// Check if running in Lambda environment
	lambdaFunctionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	fmt.Printf("AWS_LAMBDA_FUNCTION_NAME: '%s'\n", lambdaFunctionName)

	if lambdaFunctionName != "" {
		fmt.Println("✅ Detected Lambda environment - Starting Lambda handler...")
		lambda.Start(lambdaHandler)
		return
	}

	// Run the existing expired sessions check
	err := sqsCheckExpiredSessions(DynamoDbTable, Region)
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
