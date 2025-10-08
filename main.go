package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"pim-manager/pkg/dynamodb"
	"pim-manager/pkg/identitycenter"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/aws/aws-sdk-go/service/ses"
	"github.com/google/uuid"
)

var (
	expiration        dynamodb.Timestamp
	Region            string
	DynamoDbTable     string
	ApproversSecret   string
	ManagementAccount string
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

	ApproversSecret = os.Getenv("APPROVERS")
	fmt.Printf("APPROVERS environment variable: '%s'\n", ApproversSecret)
	if ApproversSecret == "" {
		fmt.Println("ERROR: APPROVERS secret name is not set!")
		panic(fmt.Errorf("APPROVERS is not set!"))
	}
	fmt.Printf("✅ APPROVERS secret set to: %s\n", ApproversSecret)

	ManagementAccount = os.Getenv("MANAGEMENT_ACCOUNT")
	fmt.Printf("MANAGEMENT_ACCOUNT environment variable: '%s'\n", ManagementAccount)
	if ManagementAccount == "" {
		fmt.Println("ERROR: MANAGEMENT_ACCOUNT is not set!")
		panic(fmt.Errorf("MANAGEMENT_ACCOUNT is not set!"))
	}
	fmt.Printf("✅ MANAGEMENT_ACCOUNT set to: %s\n", ManagementAccount)

	sessionTimeoutStr := os.Getenv("SESSION_TIMEOUT")
	fmt.Printf("SESSION_TIMEOUT environment variable: '%s'\n", sessionTimeoutStr)
	if sessionTimeoutStr == "" {
		fmt.Println("WARNING: SESSION_TIMEOUT is not set, using default 3600 seconds (1 hour)")
		expiration = 3600 // Default to 1 hour
	} else {
		timeoutSeconds, err := strconv.ParseInt(sessionTimeoutStr, 10, 64)
		if err != nil {
			fmt.Printf("ERROR: SESSION_TIMEOUT is not a valid number: %v\n", err)
			panic(fmt.Errorf("SESSION_TIMEOUT must be a valid number of seconds"))
		}
		if timeoutSeconds <= 0 {
			fmt.Printf("ERROR: SESSION_TIMEOUT must be positive, got: %d\n", timeoutSeconds)
			panic(fmt.Errorf("SESSION_TIMEOUT must be positive"))
		}
		expiration = dynamodb.Timestamp(timeoutSeconds)
	}
	fmt.Printf("✅ SESSION_TIMEOUT set to: %d seconds (%s)\n", expiration, formatDuration(int64(expiration)))
	
	fmt.Println("=== Lambda Function Initialization Complete ===")
}

// formatDuration converts seconds to human-readable format
func formatDuration(seconds int64) string {
	if seconds < 60 {
		return fmt.Sprintf("%d seconds", seconds)
	} else if seconds < 3600 {
		minutes := seconds / 60
		remainingSeconds := seconds % 60
		if remainingSeconds == 0 {
			return fmt.Sprintf("%d minutes", minutes)
		}
		return fmt.Sprintf("%d minutes %d seconds", minutes, remainingSeconds)
	} else {
		hours := seconds / 3600
		remainingMinutes := (seconds % 3600) / 60
		if remainingMinutes == 0 {
			return fmt.Sprintf("%d hours", hours)
		}
		return fmt.Sprintf("%d hours %d minutes", hours, remainingMinutes)
	}
}

// SQSMessage represents the expected structure of SQS message body
type SQSMessage struct {
	Requestor string `json:"requestor"`
	Approver  string `json:"approver"`
	Account   string `json:"account"`
	Datetime  string `json:"datetime"`
}

// getApproversFromSecret fetches the list of approved users from AWS Secrets Manager
func getApproversFromSecret(secretName, region string) ([]string, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %v", err)
	}

	svc := secretsmanager.New(sess)
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(input)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret value: %v", err)
	}

	var approvers []string
	err = json.Unmarshal([]byte(*result.SecretString), &approvers)
	if err != nil {
		return nil, fmt.Errorf("failed to parse approvers list: %v", err)
	}

	return approvers, nil
}

// validateApprover checks if the approver is in the allowed list
func validateApproverFromSecret(approver, secretName, region string) error {
	fmt.Printf("🔍 Validating approver '%s' against secret '%s'\n", approver, secretName)
	
	approvers, err := getApproversFromSecret(secretName, region)
	if err != nil {
		return fmt.Errorf("failed to fetch approvers: %v", err)
	}

	fmt.Printf("📋 Found %d approved users in secret\n", len(approvers))
	
	// Check if approver is in the list (case-insensitive)
	for _, validApprover := range approvers {
		if strings.EqualFold(strings.TrimSpace(validApprover), strings.TrimSpace(approver)) {
			fmt.Printf("✅ Approver '%s' is authorized\n", approver)
			return nil
		}
	}

	return fmt.Errorf("approver '%s' is not in the authorized approvers list", approver)
}

// validateNotManagementAccount checks if the request is for management account
func validateNotManagementAccount(accountID, managementAccount string) error {
	if strings.TrimSpace(accountID) == strings.TrimSpace(managementAccount) {
		return fmt.Errorf("access to management account '%s' is not allowed", managementAccount)
	}
	return nil
}

// sendEmailNotification sends email notification to the requestor
func sendEmailNotification(requestor, status, reason, account, region string) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return fmt.Errorf("failed to create AWS session for SES: %v", err)
	}

	svc := ses.New(sess)
	
	var subject, body string
	switch status {
	case "APPROVED":
		subject = fmt.Sprintf("✅ PIM Request Approved - Account %s", account)
		body = fmt.Sprintf(`
Your Privileged Identity Management (PIM) request has been APPROVED.

Details:
- Account: %s
- Permission Set: AdministratorAccess
- Duration: 1 hour
- Status: %s

You should now have access to the requested AWS account.

This is an automated notification from the PIM system.
		`, account, status)
	case "EXPIRED":
		subject = fmt.Sprintf("⏰ PIM Access Expired - Account %s", account)
		body = fmt.Sprintf(`
Your Privileged Identity Management (PIM) access has EXPIRED and been automatically removed.

Details:
- Account: %s
- Permission Set: AdministratorAccess
- Status: %s
- Reason: %s

Your temporary access has been revoked. If you need continued access, please submit a new request.

This is an automated notification from the PIM system.
		`, account, status, reason)
	default: // REJECTED
		subject = fmt.Sprintf("❌ PIM Request Rejected - Account %s", account)
		body = fmt.Sprintf(`
Your Privileged Identity Management (PIM) request has been REJECTED.

Details:
- Account: %s
- Status: %s
- Reason: %s

Please contact your administrator if you believe this is an error.

This is an automated notification from the PIM system.
		`, account, status, reason)
	}

	input := &ses.SendEmailInput{
		Destination: &ses.Destination{
			ToAddresses: []*string{aws.String(requestor)},
		},
		Message: &ses.Message{
			Body: &ses.Body{
				Text: &ses.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String(body),
				},
			},
			Subject: &ses.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String(subject),
			},
		},
		Source: aws.String("noreply@popai.health"), // Change to your verified SES email
	}

	_, err = svc.SendEmail(input)
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}

	fmt.Printf("📧 Email notification sent to %s (Status: %s)\n", requestor, status)
	return nil
}

// universalHandler handles both SQS and EventBridge events
func universalHandler(ctx context.Context, event interface{}) error {
	fmt.Printf("🔔 Received Lambda event\n")
	
	// Try to determine event type by checking the event structure
	eventBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %v", err)
	}
	
	// Check if it's an SQS event
	var sqsEvent events.SQSEvent
	if err := json.Unmarshal(eventBytes, &sqsEvent); err == nil && len(sqsEvent.Records) > 0 {
		// Check if first record has SQS-specific fields
		if sqsEvent.Records[0].EventSource == "aws:sqs" {
			fmt.Printf("📋 Detected SQS event with %d records\n", len(sqsEvent.Records))
			return handleSQSEvent(ctx, sqsEvent)
		}
	}
	
	// Check if it's an EventBridge event
	var eventBridgeEvent events.CloudWatchEvent
	if err := json.Unmarshal(eventBytes, &eventBridgeEvent); err == nil && eventBridgeEvent.Source != "" {
		fmt.Printf("⏰ Detected EventBridge scheduled event\n")
		return handleScheduledEvent(ctx, eventBridgeEvent)
	}
	
	// If we can't determine the event type, log the raw event
	fmt.Printf("❓ Unknown event type received: %s\n", string(eventBytes))
	return fmt.Errorf("unsupported event type")
}

// handleSQSEvent processes SQS messages for permission assignment
func handleSQSEvent(ctx context.Context, sqsEvent events.SQSEvent) error {
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
		
		var rejectionReason string
		var success bool = false
		
		// 1. Validate approver is authorized
		err := validateApproverFromSecret(message.Approver, ApproversSecret, Region)
		if err != nil {
			rejectionReason = fmt.Sprintf("Unauthorized approver: %v", err)
			fmt.Printf("❌ %s\n", rejectionReason)
		} else {
			// 2. Validate not requesting management account
			err = validateNotManagementAccount(message.Account, ManagementAccount)
			if err != nil {
				rejectionReason = fmt.Sprintf("Management account protection: %v", err)
				fmt.Printf("❌ %s\n", rejectionReason)
			} else {
				// 3. All validations passed - proceed with assignment
				pimRequest := dynamodb.DynamoDbPimRequests{
					Requester: message.Requestor,
					Approver:  message.Approver,
					AccountID: message.Account,
				}
				
				permissionSet := "AdministratorAccess"
				fmt.Printf("🔑 Assigning permission set '%s' to user '%s' for account '%s'\n", 
					permissionSet, message.Requestor, message.Account)
				
				err = sqsAssignPermissionSet(DynamoDbTable, pimRequest, permissionSet, Region)
				if err != nil {
					rejectionReason = fmt.Sprintf("Permission assignment failed: %v", err)
					fmt.Printf("❌ %s\n", rejectionReason)
				} else {
					success = true
					fmt.Printf("✅ Successfully assigned Administration access to %s for account %s\n", 
						message.Requestor, message.Account)
				}
			}
		}
		
		// 4. Send email notification
		var status string
		if success {
			status = "APPROVED"
			rejectionReason = "" // Clear reason for approved requests
		} else {
			status = "REJECTED"
		}
		
		emailErr := sendEmailNotification(message.Requestor, status, rejectionReason, message.Account, Region)
		if emailErr != nil {
			fmt.Printf("⚠️ Failed to send email notification: %v\n", emailErr)
		}
		
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

// handleScheduledEvent processes EventBridge scheduled events for cleanup
func handleScheduledEvent(ctx context.Context, event events.CloudWatchEvent) error {
	fmt.Printf("⏰ === SCHEDULED CLEANUP STARTED ===\n")
	fmt.Printf("📅 Event Source: %s\n", event.Source)
	fmt.Printf("🔍 Detail Type: %s\n", event.DetailType)
	fmt.Printf("⏰ Event Time: %s\n", event.Time.Format(time.RFC3339))
	
	// Check for expired sessions and remove permissions
	err := processExpiredSessions(DynamoDbTable, Region)
	if err != nil {
		fmt.Printf("❌ Error processing expired sessions: %v\n", err)
		return err
	}
	
	fmt.Printf("✅ Scheduled cleanup completed successfully\n")
	return nil
}

// processExpiredSessions finds and removes expired permission assignments
func processExpiredSessions(table string, region string) error {
	fmt.Printf("🔍 Checking for expired sessions in table: %s\n", table)
	
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return fmt.Errorf("failed to initialize DynamoDB: %v", err)
	}

	// Get expired records that haven't been processed yet
	expiredRecords, err := dynamo.GetExpired(false)
	if err != nil {
		return fmt.Errorf("failed to get expired records: %v", err)
	}

	if len(expiredRecords) == 0 {
		fmt.Printf("✅ No expired sessions found\n")
		return nil
	}

	fmt.Printf("🔍 Found %d expired sessions to process\n", len(expiredRecords))

	var successCount, errorCount int

	for i, record := range expiredRecords {
		fmt.Printf("\n--- Processing expired record %d/%d ---\n", i+1, len(expiredRecords))
		fmt.Printf("📋 RequestID: %s\n", record.RequestID)
		fmt.Printf("👤 User: %s\n", record.Requester)
		fmt.Printf("🏦 Account: %s\n", record.AccountID)
		fmt.Printf("✅ Approver: %s\n", record.Approver)
		fmt.Printf("⏰ Expired at: %s\n", time.Unix(int64(record.ExpirationTimestamp), 0).Format(time.RFC3339))

		// Remove permission set from Identity Center
		err := identityCenterRemovePermissionSetWithApprover(
			record.Requester, 
			record.AccountID, 
			"AdministratorAccess", 
			record.Approver, 
			region,
		)
		if err != nil {
			fmt.Printf("❌ Error removing permission set for %s: %v\n", record.Requester, err)
			errorCount++
			continue
		}

		// Mark record as expired in DynamoDB
		err = dynamo.UpdateItemStatus(record.RequestID, record.CreatedTimestamp, dynamodb.Expired)
		if err != nil {
			fmt.Printf("❌ Error updating record status to Expired: %v\n", err)
			errorCount++
			continue
		}

		successCount++
		fmt.Printf("✅ Successfully removed access for %s from account %s\n", record.Requester, record.AccountID)
		
		// Send notification email about access removal
		emailErr := sendEmailNotification(
			record.Requester, 
			"EXPIRED", 
			"Your temporary access has expired and been automatically removed", 
			record.AccountID, 
			region,
		)
		if emailErr != nil {
			fmt.Printf("⚠️ Failed to send expiration email to %s: %v\n", record.Requester, emailErr)
		}
	}

	fmt.Printf("\n📊 === CLEANUP SUMMARY ===\n")
	fmt.Printf("✅ Successfully processed: %d\n", successCount)
	fmt.Printf("❌ Errors encountered: %d\n", errorCount)
	fmt.Printf("📧 Total records processed: %d\n", len(expiredRecords))

	if errorCount > 0 {
		return fmt.Errorf("completed with %d errors out of %d records", errorCount, len(expiredRecords))
	}

	return nil
}

func main() {
	fmt.Println("=== Main Function Started ===")

	// Check if running in Lambda environment
	lambdaFunctionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	fmt.Printf("AWS_LAMBDA_FUNCTION_NAME: '%s'\n", lambdaFunctionName)

	if lambdaFunctionName != "" {
		fmt.Println("✅ Detected Lambda environment - Starting universal handler...")
		lambda.Start(universalHandler)
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
