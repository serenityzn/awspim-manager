package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"pim-manager/pkg/dynamodb"
	"pim-manager/pkg/identitycenter"
	"net/mail"
	"regexp"
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
	PermissionSet     string
	// LogLevel controls verbosity. Accepted values: "debug", "info" (default).
	// Set LOG_LEVEL=debug to include PII such as email addresses, account IDs,
	// and raw SQS message bodies in CloudWatch logs.
	LogLevel string
)

// isDebug returns true when debug-level logging is enabled.
func isDebug() bool {
	return strings.EqualFold(LogLevel, "debug")
}

func logDebug(format string, args ...interface{}) {
	if isDebug() {
		fmt.Printf("[DEBUG] "+format, args...)
	}
}

func logInfo(format string, args ...interface{}) {
	fmt.Printf("[INFO] "+format, args...)
}

func logWarn(format string, args ...interface{}) {
	fmt.Printf("[WARN] "+format, args...)
}

func logError(format string, args ...interface{}) {
	fmt.Printf("[ERROR] "+format, args...)
}

func init() {
	// Read LogLevel first so every subsequent log call respects it.
	LogLevel = strings.ToLower(strings.TrimSpace(os.Getenv("LOG_LEVEL")))
	if LogLevel == "" {
		LogLevel = "info"
	}

	logInfo("=== Lambda Function Initialization (log_level=%s) ===\n", LogLevel)

	Region = os.Getenv("AWS_REGION")
	logDebug("AWS_REGION: '%s'\n", Region)
	if Region == "" {
		logError("AWS_REGION is not set\n")
		panic(fmt.Errorf("AWS_REGION is not set!"))
	}

	DynamoDbTable = os.Getenv("DYNAMO_TABLE")
	logDebug("DYNAMO_TABLE: '%s'\n", DynamoDbTable)
	if DynamoDbTable == "" {
		logError("DYNAMO_TABLE is not set\n")
		panic(fmt.Errorf("DynamoDbTable is not set!"))
	}

	ApproversSecret = os.Getenv("APPROVERS")
	logDebug("APPROVERS secret name: '%s'\n", ApproversSecret)
	if ApproversSecret == "" {
		logError("APPROVERS secret name is not set\n")
		panic(fmt.Errorf("APPROVERS is not set!"))
	}

	ManagementAccount = os.Getenv("MANAGEMENT_ACCOUNT")
	logDebug("MANAGEMENT_ACCOUNT configured\n")
	if ManagementAccount == "" {
		logError("MANAGEMENT_ACCOUNT is not set\n")
		panic(fmt.Errorf("MANAGEMENT_ACCOUNT is not set!"))
	}

	sessionTimeoutStr := os.Getenv("SESSION_TIMEOUT")
	logDebug("SESSION_TIMEOUT raw value: '%s'\n", sessionTimeoutStr)
	if sessionTimeoutStr == "" {
		logWarn("SESSION_TIMEOUT is not set, using default 3600 seconds (1 hour)\n")
		expiration = 3600
	} else {
		timeoutSeconds, err := strconv.ParseInt(sessionTimeoutStr, 10, 64)
		if err != nil {
			logError("SESSION_TIMEOUT is not a valid number: %v\n", err)
			panic(fmt.Errorf("SESSION_TIMEOUT must be a valid number of seconds"))
		}
		if timeoutSeconds <= 0 {
			logError("SESSION_TIMEOUT must be positive, got: %d\n", timeoutSeconds)
			panic(fmt.Errorf("SESSION_TIMEOUT must be positive"))
		}
		expiration = dynamodb.Timestamp(timeoutSeconds)
	}

	PermissionSet = os.Getenv("PIM_ROLE")
	if PermissionSet == "" {
		PermissionSet = "AdministratorAccess"
		logInfo("PIM_ROLE not set, using default: %s\n", PermissionSet)
	} else {
		logInfo("PIM_ROLE set to: %s\n", PermissionSet)
	}

	logInfo("=== Lambda Function Initialized (region=%s, timeout=%s) ===\n", Region, formatDuration(int64(expiration)))
}

// formatDuration converts seconds to human-readable format.
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

// SQSMessage represents the expected structure of SQS message body.
type SQSMessage struct {
	Requestor string `json:"requestor"`
	Approver  string `json:"approver"`
	Account   string `json:"account"`
	Datetime  string `json:"datetime"`
}

// accountIDRegex validates AWS account IDs: exactly 12 digits.
var accountIDRegex = regexp.MustCompile(`^\d{12}$`)

// validateEmail checks email format using Go's standard RFC 5322 parser.
func validateEmail(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if _, err := mail.ParseAddress(value); err != nil {
		return fmt.Errorf("%s is not a valid email address: %v", field, err)
	}
	return nil
}

// validateSQSMessage checks that all required fields are present and well-formed.
func validateSQSMessage(msg SQSMessage) error {
	if err := validateEmail("requestor", msg.Requestor); err != nil {
		return err
	}
	if err := validateEmail("approver", msg.Approver); err != nil {
		return err
	}
	if strings.TrimSpace(msg.Account) == "" {
		return fmt.Errorf("account is empty")
	}
	if !accountIDRegex.MatchString(strings.TrimSpace(msg.Account)) {
		return fmt.Errorf("account must be a 12-digit AWS account ID")
	}
	return nil
}

// getApproversFromSecret fetches the list of approved users from AWS Secrets Manager.
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

// validateApproverFromSecret checks if the approver is in the allowed list stored in Secrets Manager.
func validateApproverFromSecret(approver, secretName, region string) error {
	logDebug("Validating approver '%s' against secret '%s'\n", approver, secretName)

	approvers, err := getApproversFromSecret(secretName, region)
	if err != nil {
		return fmt.Errorf("failed to fetch approvers: %v", err)
	}

	logDebug("Found %d approved users in secret\n", len(approvers))

	for _, validApprover := range approvers {
		if strings.EqualFold(strings.TrimSpace(validApprover), strings.TrimSpace(approver)) {
			logDebug("Approver '%s' is authorized\n", approver)
			return nil
		}
	}

	return fmt.Errorf("approver '%s' is not in the authorized approvers list", approver)
}

// validateNotManagementAccount checks if the request is for management account.
func validateNotManagementAccount(accountID, managementAccount string) error {
	if strings.TrimSpace(accountID) == strings.TrimSpace(managementAccount) {
		return fmt.Errorf("access to management account is not allowed")
	}
	return nil
}

// sendEmailNotification sends email notification to the requestor.
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
- Permission Set: %s
- Duration: 1 hour
- Status: %s

You should now have access to the requested AWS account.

This is an automated notification from the PIM system.
		`, account, PermissionSet, status)
	case "EXPIRED":
		subject = fmt.Sprintf("⏰ PIM Access Expired - Account %s", account)
		body = fmt.Sprintf(`
Your Privileged Identity Management (PIM) access has EXPIRED and been automatically removed.

Details:
- Account: %s
- Permission Set: %s
- Status: %s
- Reason: %s

Your temporary access has been revoked. If you need continued access, please submit a new request.

This is an automated notification from the PIM system.
		`, account, PermissionSet, status, reason)
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

	// Email address is PII — keep recipient out of INFO logs.
	logDebug("Email notification sent to '%s' (status=%s)\n", requestor, status)
	return nil
}

// universalHandler handles both SQS and EventBridge events.
func universalHandler(ctx context.Context, event interface{}) error {
	logInfo("Received Lambda event\n")

	eventBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %v", err)
	}

	// Check if it's an SQS event.
	var sqsEvent events.SQSEvent
	if err := json.Unmarshal(eventBytes, &sqsEvent); err == nil && len(sqsEvent.Records) > 0 {
		if sqsEvent.Records[0].EventSource == "aws:sqs" {
			logInfo("Detected SQS event with %d records\n", len(sqsEvent.Records))
			return handleSQSEvent(ctx, sqsEvent)
		}
	}

	// Check if it's an EventBridge event.
	var eventBridgeEvent events.CloudWatchEvent
	if err := json.Unmarshal(eventBytes, &eventBridgeEvent); err == nil && eventBridgeEvent.Source != "" {
		logInfo("Detected EventBridge scheduled event\n")
		return handleScheduledEvent(ctx, eventBridgeEvent)
	}

	// Raw event payload may contain PII — only emit under debug.
	logDebug("Unknown event type received: %s\n", string(eventBytes))
	logError("Unsupported event type\n")
	return fmt.Errorf("unsupported event type")
}

// handleSQSEvent processes SQS messages for permission assignment.
func handleSQSEvent(ctx context.Context, sqsEvent events.SQSEvent) error {
	logInfo("Processing %d SQS record(s)\n", len(sqsEvent.Records))

	for i, record := range sqsEvent.Records {
		logInfo("Processing record %d/%d (messageId=%s)\n", i+1, len(sqsEvent.Records), record.MessageId)
		// Raw body and routing metadata contain PII — debug only.
		logDebug("EventSource=%s EventSourceARN=%s\n", record.EventSource, record.EventSourceARN)
		logDebug("Raw body: %s\n", record.Body)

		var message SQSMessage
		if err := json.Unmarshal([]byte(record.Body), &message); err != nil {
			logError("Failed to parse SQS message body (messageId=%s): %v\n", record.MessageId, err)
			logDebug("Raw body that failed to parse: %s\n", record.Body)
			continue
		}

		if err := validateSQSMessage(message); err != nil {
			logError("Invalid SQS message (messageId=%s): %v\n", record.MessageId, err)
			continue
		}

		// All fields contain PII or account metadata — debug only.
		logDebug("Requestor=%s Approver=%s Account=%s Datetime=%s\n",
			message.Requestor, message.Approver, message.Account, message.Datetime)
		if isDebug() {
			prettyBytes, _ := json.MarshalIndent(message, "", "  ")
			logDebug("Structured message:\n%s\n", string(prettyBytes))
		}

		var rejectionReason string
		var success bool

		// 1. Validate approver is authorized.
		err := validateApproverFromSecret(message.Approver, ApproversSecret, Region)
		if err != nil {
			rejectionReason = fmt.Sprintf("Unauthorized approver: %v", err)
			logInfo("Request rejected (messageId=%s): unauthorized approver\n", record.MessageId)
			logDebug("Rejection detail: %s\n", rejectionReason)
		} else {
			// 2. Validate not requesting management account.
			err = validateNotManagementAccount(message.Account, ManagementAccount)
			if err != nil {
				rejectionReason = fmt.Sprintf("Management account protection: %v", err)
				logInfo("Request rejected (messageId=%s): management account protection\n", record.MessageId)
				logDebug("Rejection detail: %s\n", rejectionReason)
			} else {
				// 3. All validations passed — proceed with assignment.
				pimRequest := dynamodb.DynamoDbPimRequests{
					Requester: message.Requestor,
					Approver:  message.Approver,
					AccountID: message.Account,
				}

				permissionSet := PermissionSet
				logDebug("Assigning permission set '%s' to requestor for account (messageId=%s)\n",
					permissionSet, record.MessageId)

				err = sqsAssignPermissionSet(DynamoDbTable, pimRequest, permissionSet, Region)
				if err != nil {
					rejectionReason = fmt.Sprintf("Permission assignment failed: %v", err)
					logInfo("Request failed (messageId=%s): permission assignment error\n", record.MessageId)
					logDebug("Error detail: %s\n", rejectionReason)
				} else {
					success = true
					logInfo("Access granted (messageId=%s)\n", record.MessageId)
				}
			}
		}

		// 4. Send email notification.
		var status string
		if success {
			status = "APPROVED"
			rejectionReason = ""
		} else {
			status = "REJECTED"
		}

		emailErr := sendEmailNotification(message.Requestor, status, rejectionReason, message.Account, Region)
		if emailErr != nil {
			logWarn("Failed to send email notification (messageId=%s): %v\n", record.MessageId, emailErr)
		}

		if len(record.MessageAttributes) > 0 {
			logDebug("Message has %d attribute(s)\n", len(record.MessageAttributes))
			for key, attr := range record.MessageAttributes {
				var value string
				if attr.StringValue != nil {
					value = *attr.StringValue
				}
				logDebug("  Attribute %s=%s (type=%s)\n", key, value, attr.DataType)
			}
		}
	}

	logInfo("Finished processing %d SQS record(s)\n", len(sqsEvent.Records))
	return nil
}

// handleScheduledEvent processes EventBridge scheduled events for cleanup.
func handleScheduledEvent(ctx context.Context, event events.CloudWatchEvent) error {
	logInfo("Scheduled cleanup started (event_time=%s)\n", event.Time.Format(time.RFC3339))
	logDebug("EventBridge source=%s detail_type=%s\n", event.Source, event.DetailType)

	err := processExpiredSessions(DynamoDbTable, Region)
	if err != nil {
		logError("Error processing expired sessions: %v\n", err)
		return err
	}

	logInfo("Scheduled cleanup completed successfully\n")
	return nil
}

// processExpiredSessions finds and removes expired permission assignments.
func processExpiredSessions(table string, region string) error {
	logInfo("Checking for expired sessions\n")

	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return fmt.Errorf("failed to initialize DynamoDB: %v", err)
	}

	expiredRecords, err := dynamo.GetExpired(false)
	if err != nil {
		return fmt.Errorf("failed to get expired records: %v", err)
	}

	if len(expiredRecords) == 0 {
		logInfo("No expired sessions found\n")
		return nil
	}

	logInfo("Found %d expired session(s) to process\n", len(expiredRecords))

	var successCount, errorCount int

	for i, record := range expiredRecords {
		logInfo("Processing expired record %d/%d (requestId=%s)\n", i+1, len(expiredRecords), record.RequestID)
		// User email, account ID, and approver are PII — debug only.
		logDebug("Requester=%s Account=%s Approver=%s ExpiredAt=%s\n",
			record.Requester, record.AccountID, record.Approver,
			time.Unix(int64(record.ExpirationTimestamp), 0).Format(time.RFC3339))

		err := identityCenterRemovePermissionSetWithApprover(
			record.Requester,
			record.AccountID,
			PermissionSet,
			record.Approver,
			region,
		)
		if err != nil {
			logError("Failed to remove permission set (requestId=%s): %v\n", record.RequestID, err)
			errorCount++
			continue
		}

		err = dynamo.UpdateItemStatus(record.RequestID, record.CreatedTimestamp, dynamodb.Expired)
		if err != nil {
			logError("Failed to update record status (requestId=%s): %v\n", record.RequestID, err)
			errorCount++
			continue
		}

		successCount++
		logInfo("Access revoked (requestId=%s)\n", record.RequestID)

		emailErr := sendEmailNotification(
			record.Requester,
			"EXPIRED",
			"Your temporary access has expired and been automatically removed",
			record.AccountID,
			region,
		)
		if emailErr != nil {
			logWarn("Failed to send expiration email (requestId=%s): %v\n", record.RequestID, emailErr)
		}
	}

	logInfo("Cleanup summary: succeeded=%d errors=%d total=%d\n", successCount, errorCount, len(expiredRecords))

	if errorCount > 0 {
		return fmt.Errorf("completed with %d errors out of %d records", errorCount, len(expiredRecords))
	}

	return nil
}

func main() {
	logInfo("Starting\n")

	lambdaFunctionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	logDebug("AWS_LAMBDA_FUNCTION_NAME: '%s'\n", lambdaFunctionName)

	if lambdaFunctionName != "" {
		logInfo("Lambda environment detected — starting universal handler\n")
		lambda.Start(universalHandler)
		return
	}

	logInfo("Local mode — running expired sessions cleanup\n")
	err := sqsCheckExpiredSessions(DynamoDbTable, Region)
	if err != nil {
		logError("Error checking expired sessions: %v\n", err)
		panic(err)
	}
}

func sqsCheckExpiredSessions(table string, region string) error {
	err := updateExpiredSessions(table, region)
	if err != nil {
		logError("Error updating expired sessions: %v\n", err)
		return err
	}

	return nil
}

func dynamoDbNewRequest(table string, item dynamodb.DynamoDbPimRequests) error {
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	logDebug("Writing item to DynamoDB (requestId=%s)\n", item.RequestID)

	err = dynamo.WriteItem(item)
	if err != nil {
		logError("Error writing item to DynamoDB: %v\n", err)
		return err
	}

	logDebug("DynamoDB item written successfully (requestId=%s)\n", item.RequestID)
	return nil
}

func dynamoDbUpdateRequestStatus(table string, status dynamodb.RequestStatus, requestId string) error {
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	valueTimestamp, err := dynamo.GetCreatedTimestamp(requestId)
	if err != nil {
		logError("Error getting created timestamp (requestId=%s): %v\n", requestId, err)
		return err
	}
	logDebug("Created timestamp retrieved (requestId=%s)\n", requestId)

	err = dynamo.UpdateItemStatus(requestId, valueTimestamp, status)
	if err != nil {
		logError("Error updating item status (requestId=%s): %v\n", requestId, err)
		return err
	}

	logDebug("Item status updated (requestId=%s, status=%s)\n", requestId, status)
	return nil
}

func dynamoDbInitialize(table string) (*dynamodb.DynamoDbConfig, error) {
	var awsConfig dynamodb.AwsConfig
	awsConfig.Region = Region

	logDebug("Creating DynamoDB config (table=%s, region=%s)\n", table, Region)
	dynamo, err := dynamodb.NewDynamoDbConfig(awsConfig, table)
	if err != nil {
		logError("Error creating DynamoDB config: %v\n", err)
		return nil, err
	}

	logDebug("Checking if DynamoDB table exists\n")
	err = dynamo.CheckTableExists()
	if err != nil {
		logError("Error checking DynamoDB table '%s': %v\n", table, err)
		return nil, err
	}

	logDebug("DynamoDB table verified\n")
	return dynamo, nil
}

func updateExpiredSessions(table string, region string) error {
	var count int
	dynamo, err := dynamoDbInitialize(table)
	if err != nil {
		return err
	}

	expiredRecords, err := dynamo.GetExpired(false)
	if err != nil {
		return err
	}

	logInfo("Found %d expired record(s) to process\n", len(expiredRecords))
	// Full record struct contains PII — debug only.
	logDebug("Expired records: %v\n", expiredRecords)

	for i := range expiredRecords {
		logInfo("Processing record %d/%d (requestId=%s)\n", i+1, len(expiredRecords), expiredRecords[i].RequestID)
		err := identityCenterRemovePermissionSetWithApprover(
			expiredRecords[i].Requester, expiredRecords[i].AccountID, PermissionSet,
			expiredRecords[i].Approver, region)
		if err != nil {
			logError("Error removing permission set (requestId=%s): %v\n", expiredRecords[i].RequestID, err)
			return err
		}

		err = dynamo.UpdateItemStatus(expiredRecords[i].RequestID, expiredRecords[i].CreatedTimestamp, dynamodb.Expired)
		if err != nil {
			logError("Error updating item status (requestId=%s): %v\n", expiredRecords[i].RequestID, err)
			return err
		}
		count++
		logInfo("Record marked as expired (%d/%d, requestId=%s)\n", count, len(expiredRecords), expiredRecords[i].RequestID)
	}

	return nil
}

func identityCenterInitialize(region string) (*identitycenter.IdentityCenterConfig, error) {
	ic, err := identitycenter.NewIdentityCenterConfig(region)
	if err != nil {
		logError("Error creating Identity Center config: %v\n", err)
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
		logError("Error assigning permission set: %v\n", err)
		return err
	}

	return nil
}

func validateApprover(approverEmail string, region string) error {
	ic, err := identityCenterInitialize(region)
	if err != nil {
		return err
	}

	logDebug("Validating approver exists in Identity Center\n")
	_, err = ic.FindUserByEmail(approverEmail)
	if err != nil {
		return fmt.Errorf("approver validation failed: approver is not a valid user in the organization: %v", err)
	}

	logDebug("Approver validated in Identity Center\n")
	return nil
}

func identityCenterRemovePermissionSet(email, accountID, permissionSet string, region string) error {
	ic, err := identityCenterInitialize(region)
	if err != nil {
		return err
	}

	err = ic.RemoveUserFromAccountByEmail(email, accountID, permissionSet)
	if err != nil {
		logError("Error removing permission set: %v\n", err)
		return err
	}

	return nil
}

func identityCenterRemovePermissionSetWithApprover(email, accountID, permissionSet, approverEmail, region string) error {
	err := validateApprover(approverEmail, region)
	if err != nil {
		logWarn("Skipping permission removal — approver validation failed: %v\n", err)
		return err
	}

	return identityCenterRemovePermissionSet(email, accountID, permissionSet, region)
}

func sqsAssignPermissionSet(table string, item dynamodb.DynamoDbPimRequests, permissionSet string, region string) error {
	err := validateApprover(item.Approver, region)
	if err != nil {
		logWarn("Skipping permission assignment — approver validation failed: %v\n", err)
		return err
	}

	err = identityCenterAssignPermissionSet(item.Requester, item.AccountID, permissionSet, region)
	if err != nil {
		logError("Error assigning permission set: %v\n", err)
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
		logError("Error storing request in DynamoDB: %v\n", err)
		return err
	}

	return nil
}
