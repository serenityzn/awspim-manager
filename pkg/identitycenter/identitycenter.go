package identitycenter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/identitystore"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/ssoadmin"
)

type IdentityCenterConfig struct {
	ssoAdminClient      *ssoadmin.SSOAdmin
	identityStoreClient *identitystore.IdentityStore
	organizationsClient *organizations.Organizations
	region              string
	instanceArn         string
	identityStoreId     string
	adminEmails         map[string]bool
}

type User struct {
	UserID      string
	UserName    string
	DisplayName string
	Email       string
	FirstName   string
	LastName    string
	Active      bool
	UserState   string // ACTIVE, INACTIVE, etc.
}

func NewIdentityCenterConfig(region string) (*IdentityCenterConfig, error) {
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(region),
		},
		SharedConfigState: session.SharedConfigEnable,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %v", err)
	}

	config := &IdentityCenterConfig{
		ssoAdminClient:      ssoadmin.New(sess),
		identityStoreClient: identitystore.New(sess),
		organizationsClient: organizations.New(sess),
		region:              region,
		adminEmails:         loadAdminEmails(),
	}

	// Pre-fetch SSO instance details for efficiency
	err = config.initializeInstanceDetails()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize SSO instance details: %v", err)
	}

	return config, nil
}

// loadAdminEmails loads admin emails from environment variable
func loadAdminEmails() map[string]bool {
	adminUsers := os.Getenv("adminUsers")
	adminMap := make(map[string]bool)

	if adminUsers != "" {
		emails := strings.Split(adminUsers, ",")
		for _, email := range emails {
			trimmedEmail := strings.TrimSpace(email)
			if trimmedEmail != "" {
				adminMap[strings.ToLower(trimmedEmail)] = true
				fmt.Printf("Protected admin user: %s\n", trimmedEmail)
			}
		}
		fmt.Printf("Loaded %d admin users for protection\n", len(adminMap))
	} else {
		fmt.Println("No admin users configured (adminUsers env var not set)")
	}

	return adminMap
}

// isAdminUser checks if a user email is in the protected admin list
func (ic *IdentityCenterConfig) isAdminUser(email string) bool {
	if email == "" {
		return false
	}
	return ic.adminEmails[strings.ToLower(email)]
}

// validateNotAdminUser checks if operation should be blocked for admin users
func (ic *IdentityCenterConfig) validateNotAdminUser(email, operation string) error {
	if ic.isAdminUser(email) {
		return fmt.Errorf("operation '%s' blocked: user %s is a protected admin user", operation, email)
	}
	return nil
}

func (ic *IdentityCenterConfig) initializeInstanceDetails() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &ssoadmin.ListInstancesInput{}
	result, err := ic.ssoAdminClient.ListInstancesWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to list SSO instances: %v", err)
	}

	if len(result.Instances) == 0 {
		return fmt.Errorf("no SSO instances found")
	}

	ic.instanceArn = *result.Instances[0].InstanceArn
	ic.identityStoreId = *result.Instances[0].IdentityStoreId

	return nil
}

func (ic *IdentityCenterConfig) GetIdentityStoreID() (string, error) {
	if ic.identityStoreId != "" {
		return ic.identityStoreId, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List SSO instances to get the identity store ID
	input := &ssoadmin.ListInstancesInput{}

	result, err := ic.ssoAdminClient.ListInstancesWithContext(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to list SSO instances: %v", err)
	}

	if len(result.Instances) == 0 {
		return "", fmt.Errorf("no SSO instances found")
	}

	// Return the identity store ID from the first (and typically only) instance
	return *result.Instances[0].IdentityStoreId, nil
}

func (ic *IdentityCenterConfig) ListUsers() ([]User, error) {
	return ic.ListUsersWithFilter("")
}

func (ic *IdentityCenterConfig) ListEnabledUsers() ([]User, error) {
	return ic.ListUsersWithFilter("ACTIVE")
}

func (ic *IdentityCenterConfig) ListUsersWithFilter(filterUserState string) ([]User, error) {
	// First get the identity store ID
	identityStoreID, err := ic.GetIdentityStoreID()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var allUsers []User

	input := &identitystore.ListUsersInput{
		IdentityStoreId: aws.String(identityStoreID),
	}

	// Add filter if userState is specified
	if filterUserState != "" {
		input.Filters = []*identitystore.Filter{
			{
				AttributePath:  aws.String("UserState"),
				AttributeValue: aws.String(filterUserState),
			},
		}
	}

	// Use pagination to get all users
	err = ic.identityStoreClient.ListUsersPagesWithContext(ctx, input, func(page *identitystore.ListUsersOutput, lastPage bool) bool {
		for _, user := range page.Users {
			u := User{
				UserID: aws.StringValue(user.UserId),
				Active: true, // Will be updated based on UserState
			}

			// Parse UserName
			if user.UserName != nil {
				u.UserName = *user.UserName
			}

			// Parse DisplayName
			if user.DisplayName != nil {
				u.DisplayName = *user.DisplayName
			}

			// Parse Name (first and last name)
			if user.Name != nil {
				if user.Name.GivenName != nil {
					u.FirstName = *user.Name.GivenName
				}
				if user.Name.FamilyName != nil {
					u.LastName = *user.Name.FamilyName
				}
			}

			// Parse Emails (get primary email)
			if len(user.Emails) > 0 {
				for _, email := range user.Emails {
					if email.Value != nil {
						u.Email = *email.Value
						break // Use the first email found
					}
				}
			}

			// Set UserState based on filter applied
			// If we filtered for ACTIVE users, they are active
			// If no filter was applied, assume active (since inactive users may not be returned)
			if filterUserState != "" {
				u.UserState = filterUserState
				u.Active = (filterUserState == "ACTIVE")
			} else {
				u.UserState = "ACTIVE" // Default assumption
				u.Active = true
			}

			allUsers = append(allUsers, u)
		}

		return !lastPage
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list users: %v", err)
	}

	return allUsers, nil
}

func (ic *IdentityCenterConfig) GetUser(userID string) (*User, error) {
	// First get the identity store ID
	identityStoreID, err := ic.GetIdentityStoreID()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &identitystore.DescribeUserInput{
		IdentityStoreId: aws.String(identityStoreID),
		UserId:          aws.String(userID),
	}

	result, err := ic.identityStoreClient.DescribeUserWithContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to get user %s: %v", userID, err)
	}

	user := &User{
		UserID: aws.StringValue(result.UserId),
		Active: true, // Will be updated based on UserState
	}

	// Parse UserName
	if result.UserName != nil {
		user.UserName = *result.UserName
	}

	// Parse DisplayName
	if result.DisplayName != nil {
		user.DisplayName = *result.DisplayName
	}

	// Parse Name
	if result.Name != nil {
		if result.Name.GivenName != nil {
			user.FirstName = *result.Name.GivenName
		}
		if result.Name.FamilyName != nil {
			user.LastName = *result.Name.FamilyName
		}
	}

	// Parse Emails
	if len(result.Emails) > 0 {
		for _, email := range result.Emails {
			if email.Value != nil {
				user.Email = *email.Value
				break
			}
		}
	}

	// Set default UserState - AWS Identity Store typically only returns active users
	// If you got this user from the API, they are likely active
	user.UserState = "ACTIVE"
	user.Active = true

	return user, nil
}

// FindUserByEmail finds a user by their email address and returns their User ID
func (ic *IdentityCenterConfig) FindUserByEmail(email string) (string, error) {
	users, err := ic.ListUsers()
	if err != nil {
		return "", fmt.Errorf("failed to list users: %v", err)
	}

	for _, user := range users {
		if user.Email == email {
			return user.UserID, nil
		}
	}

	return "", fmt.Errorf("user with email '%s' not found", email)
}

// FindUserByUsername finds a user by their username and returns their User ID
func (ic *IdentityCenterConfig) FindUserByUsername(username string) (string, error) {
	users, err := ic.ListUsers()
	if err != nil {
		return "", fmt.Errorf("failed to list users: %v", err)
	}

	for _, user := range users {
		if user.UserName == username {
			return user.UserID, nil
		}
	}

	return "", fmt.Errorf("user with username '%s' not found", username)
}

// AssignUserToAccountByEmail assigns a user to an account using email address instead of User ID
func (ic *IdentityCenterConfig) AssignUserToAccountByEmail(email, accountID, permissionSetIdentifier string) error {
	// Check if user is protected admin
	err := ic.validateNotAdminUser(email, "assign permission")
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
		return err
	}

	// First find the user ID by email
	fmt.Printf("Finding user by email: %s...\n", email)
	userID, err := ic.FindUserByEmail(email)
	if err != nil {
		return fmt.Errorf("failed to find user by email: %v", err)
	}

	fmt.Printf("Found user ID: %s for email: %s\n", userID, email)

	// Now use the regular assignment function with the User ID
	return ic.AssignUserToAccount(userID, accountID, permissionSetIdentifier)
}

// AssignUserToAccountByUsername assigns a user to an account using username instead of User ID
func (ic *IdentityCenterConfig) AssignUserToAccountByUsername(username, accountID, permissionSetIdentifier string) error {
	// First find the user to get their email for admin check
	fmt.Printf("Finding user by username: %s...\n", username)
	userID, err := ic.FindUserByUsername(username)
	if err != nil {
		return fmt.Errorf("failed to find user by username: %v", err)
	}

	// Get user details to check email for admin protection
	user, err := ic.GetUser(userID)
	if err != nil {
		return fmt.Errorf("failed to get user details: %v", err)
	}

	// Check if user is protected admin
	err = ic.validateNotAdminUser(user.Email, "assign permission")
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
		return err
	}

	fmt.Printf("Found user ID: %s for username: %s\n", userID, username)

	// Now use the regular assignment function with the User ID
	return ic.AssignUserToAccount(userID, accountID, permissionSetIdentifier)
}

// ValidateAccount checks if the account exists in the organization
func (ic *IdentityCenterConfig) ValidateAccount(accountID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &organizations.DescribeAccountInput{
		AccountId: aws.String(accountID),
	}

	_, err := ic.organizationsClient.DescribeAccountWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("account %s not found in organization: %v", accountID, err)
	}

	return nil
}

// ValidateUser checks if the user exists in Identity Store
func (ic *IdentityCenterConfig) ValidateUser(userID string) error {
	_, err := ic.GetUser(userID)
	if err != nil {
		return fmt.Errorf("user %s not found: %v", userID, err)
	}
	return nil
}

// ValidatePermissionSet checks if the permission set exists
func (ic *IdentityCenterConfig) ValidatePermissionSet(permissionSetArn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &ssoadmin.DescribePermissionSetInput{
		InstanceArn:      aws.String(ic.instanceArn),
		PermissionSetArn: aws.String(permissionSetArn),
	}

	_, err := ic.ssoAdminClient.DescribePermissionSetWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("permission set %s not found: %v", permissionSetArn, err)
	}

	return nil
}

// FindPermissionSetByName finds a permission set by its name
func (ic *IdentityCenterConfig) FindPermissionSetByName(permissionSetName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &ssoadmin.ListPermissionSetsInput{
		InstanceArn: aws.String(ic.instanceArn),
	}

	var permissionSetArn string
	err := ic.ssoAdminClient.ListPermissionSetsPagesWithContext(ctx, input, func(page *ssoadmin.ListPermissionSetsOutput, lastPage bool) bool {
		for _, psArn := range page.PermissionSets {
			// Get permission set details to check the name
			describeInput := &ssoadmin.DescribePermissionSetInput{
				InstanceArn:      aws.String(ic.instanceArn),
				PermissionSetArn: psArn,
			}

			result, err := ic.ssoAdminClient.DescribePermissionSetWithContext(ctx, describeInput)
			if err == nil && result.PermissionSet.Name != nil && *result.PermissionSet.Name == permissionSetName {
				permissionSetArn = *psArn
				return false // Stop pagination
			}
		}
		return !lastPage
	})

	if err != nil {
		return "", fmt.Errorf("failed to list permission sets: %v", err)
	}

	if permissionSetArn == "" {
		return "", fmt.Errorf("permission set with name '%s' not found", permissionSetName)
	}

	return permissionSetArn, nil
}

// AssignUserToAccount assigns a user to an account with a specific permission set
func (ic *IdentityCenterConfig) AssignUserToAccount(userID, accountID, permissionSetIdentifier string) error {
	// Step 1: Validate account exists in organization
	fmt.Printf("Validating account %s...\n", accountID)
	err := ic.ValidateAccount(accountID)
	if err != nil {
		return fmt.Errorf("account validation failed: %v", err)
	}

	// Step 2: Validate user exists in Identity Store
	fmt.Printf("Validating user %s...\n", userID)
	err = ic.ValidateUser(userID)
	if err != nil {
		return fmt.Errorf("user validation failed: %v", err)
	}

	// Step 3: Get permission set ARN (either directly or by name)
	var permissionSetArn string
	if isArn(permissionSetIdentifier) {
		// It's already an ARN, validate it exists
		fmt.Printf("Validating permission set ARN %s...\n", permissionSetIdentifier)
		err = ic.ValidatePermissionSet(permissionSetIdentifier)
		if err != nil {
			return fmt.Errorf("permission set validation failed: %v", err)
		}
		permissionSetArn = permissionSetIdentifier
	} else {
		// It's a name, find the ARN
		fmt.Printf("Finding permission set by name '%s'...\n", permissionSetIdentifier)
		permissionSetArn, err = ic.FindPermissionSetByName(permissionSetIdentifier)
		if err != nil {
			return fmt.Errorf("permission set lookup failed: %v", err)
		}
	}

	// Step 4: Check if assignment already exists
	fmt.Printf("Checking if assignment already exists...\n")
	exists, err := ic.checkAssignmentExists(userID, accountID, permissionSetArn)
	if err != nil {
		return fmt.Errorf("failed to check existing assignment: %v", err)
	}

	if exists {
		fmt.Printf("Assignment already exists for user %s to account %s with permission set %s\n", userID, accountID, permissionSetArn)
		return fmt.Errorf("Assignment already exists for user %s to account %s with permission set %s\n", userID, accountID, permissionSetArn)
	}

	// Step 5: Create the assignment
	fmt.Printf("Creating assignment...\n")
	err = ic.createAccountAssignment(userID, accountID, permissionSetArn)
	if err != nil {
		return fmt.Errorf("failed to create assignment: %v", err)
	}

	fmt.Printf("Successfully assigned user %s to account %s with permission set %s\n", userID, accountID, permissionSetArn)
	return nil
}

// checkAssignmentExists checks if an assignment already exists
func (ic *IdentityCenterConfig) checkAssignmentExists(userID, accountID, permissionSetArn string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	input := &ssoadmin.ListAccountAssignmentsInput{
		InstanceArn:      aws.String(ic.instanceArn),
		AccountId:        aws.String(accountID),
		PermissionSetArn: aws.String(permissionSetArn),
	}

	var exists bool
	err := ic.ssoAdminClient.ListAccountAssignmentsPagesWithContext(ctx, input, func(page *ssoadmin.ListAccountAssignmentsOutput, lastPage bool) bool {
		for _, assignment := range page.AccountAssignments {
			if assignment.PrincipalType != nil && *assignment.PrincipalType == "USER" &&
				assignment.PrincipalId != nil && *assignment.PrincipalId == userID {
				exists = true
				return false // Stop pagination
			}
		}
		return !lastPage
	})

	return exists, err
}

// createAccountAssignment creates the actual assignment
func (ic *IdentityCenterConfig) createAccountAssignment(userID, accountID, permissionSetArn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := &ssoadmin.CreateAccountAssignmentInput{
		InstanceArn:      aws.String(ic.instanceArn),
		TargetId:         aws.String(accountID),
		TargetType:       aws.String("AWS_ACCOUNT"),
		PermissionSetArn: aws.String(permissionSetArn),
		PrincipalType:    aws.String("USER"),
		PrincipalId:      aws.String(userID),
	}

	result, err := ic.ssoAdminClient.CreateAccountAssignmentWithContext(ctx, input)
	if err != nil {
		return err
	}

	// Wait for the assignment to complete
	if result.AccountAssignmentCreationStatus != nil && result.AccountAssignmentCreationStatus.RequestId != nil {
		return ic.waitForAssignmentCompletion(*result.AccountAssignmentCreationStatus.RequestId)
	}

	return nil
}

// waitForAssignmentCompletion waits for the assignment operation to complete
func (ic *IdentityCenterConfig) waitForAssignmentCompletion(requestID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("Waiting for assignment operation to complete (RequestID: %s)...\n", requestID)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for assignment completion")
		default:
			input := &ssoadmin.DescribeAccountAssignmentCreationStatusInput{
				InstanceArn:                        aws.String(ic.instanceArn),
				AccountAssignmentCreationRequestId: aws.String(requestID),
			}

			result, err := ic.ssoAdminClient.DescribeAccountAssignmentCreationStatusWithContext(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to check assignment status: %v", err)
			}

			if result.AccountAssignmentCreationStatus != nil {
				status := *result.AccountAssignmentCreationStatus.Status
				fmt.Printf("Assignment status: %s\n", status)

				switch status {
				case "SUCCEEDED":
					return nil
				case "FAILED":
					failureReason := "Unknown error"
					if result.AccountAssignmentCreationStatus.FailureReason != nil {
						failureReason = *result.AccountAssignmentCreationStatus.FailureReason
					}
					return fmt.Errorf("assignment failed: %s", failureReason)
				case "IN_PROGRESS":
					time.Sleep(2 * time.Second)
					continue
				default:
					return fmt.Errorf("unexpected assignment status: %s", status)
				}
			}
		}
	}
}

// RemoveUserFromAccount removes a user's permission set assignment from an account
func (ic *IdentityCenterConfig) RemoveUserFromAccount(userID, accountID, permissionSetIdentifier string) error {
	// Step 1: Validate user exists in Identity Store
	fmt.Printf("Validating user %s...\n", userID)
	err := ic.ValidateUser(userID)
	if err != nil {
		return fmt.Errorf("user validation failed: %v", err)
	}

	// Step 2: Get permission set ARN (either directly or by name)
	var permissionSetArn string
	if isArn(permissionSetIdentifier) {
		// It's already an ARN, validate it exists
		fmt.Printf("Validating permission set ARN %s...\n", permissionSetIdentifier)
		err = ic.ValidatePermissionSet(permissionSetIdentifier)
		if err != nil {
			return fmt.Errorf("permission set validation failed: %v", err)
		}
		permissionSetArn = permissionSetIdentifier
	} else {
		// It's a name, find the ARN
		fmt.Printf("Finding permission set by name '%s'...\n", permissionSetIdentifier)
		permissionSetArn, err = ic.FindPermissionSetByName(permissionSetIdentifier)
		if err != nil {
			return fmt.Errorf("permission set lookup failed: %v", err)
		}
	}

	// Step 3: Check if assignment exists
	fmt.Printf("Checking if assignment exists for user %s...\n", userID)
	exists, err := ic.checkAssignmentExists(userID, accountID, permissionSetArn)
	if err != nil {
		return fmt.Errorf("failed to check existing assignment: %v", err)
	}

	if !exists {
		fmt.Printf("No assignment found for user %s to account %s with permission set %s. Nothing to remove.\n", userID, accountID, permissionSetArn)
		return nil
	}

	// Step 4: Remove the assignment
	fmt.Printf("Removing assignment...\n")
	err = ic.deleteAccountAssignment(userID, accountID, permissionSetArn)
	if err != nil {
		return fmt.Errorf("failed to remove assignment: %v", err)
	}

	fmt.Printf("Successfully removed user %s from account %s with permission set %s\n", userID, accountID, permissionSetArn)
	return nil
}

// RemoveUserFromAccountByEmail removes a user's permission set assignment using email address
func (ic *IdentityCenterConfig) RemoveUserFromAccountByEmail(email, accountID, permissionSetIdentifier string) error {
	// Check if user is protected admin
	err := ic.validateNotAdminUser(email, "remove permission")
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
		return err
	}

	// First find the user ID by email
	fmt.Printf("Finding user by email: %s...\n", email)
	userID, err := ic.FindUserByEmail(email)
	if err != nil {
		return fmt.Errorf("failed to find user by email: %v", err)
	}

	fmt.Printf("Found user ID: %s for email: %s\n", userID, email)

	// Now use the regular removal function with the User ID
	return ic.RemoveUserFromAccount(userID, accountID, permissionSetIdentifier)
}

// RemoveUserFromAccountByUsername removes a user's permission set assignment using username
func (ic *IdentityCenterConfig) RemoveUserFromAccountByUsername(username, accountID, permissionSetIdentifier string) error {
	// First find the user ID by username
	fmt.Printf("Finding user by username: %s...\n", username)
	userID, err := ic.FindUserByUsername(username)
	if err != nil {
		return fmt.Errorf("failed to find user by username: %v", err)
	}

	// Get user details to check email for admin protection
	user, err := ic.GetUser(userID)
	if err != nil {
		return fmt.Errorf("failed to get user details: %v", err)
	}

	// Check if user is protected admin
	err = ic.validateNotAdminUser(user.Email, "remove permission")
	if err != nil {
		fmt.Printf("⚠️  %v\n", err)
		return err
	}

	fmt.Printf("Found user ID: %s for username: %s\n", userID, username)

	// Now use the regular removal function with the User ID
	return ic.RemoveUserFromAccount(userID, accountID, permissionSetIdentifier)
}

// deleteAccountAssignment deletes the actual assignment
func (ic *IdentityCenterConfig) deleteAccountAssignment(userID, accountID, permissionSetArn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := &ssoadmin.DeleteAccountAssignmentInput{
		InstanceArn:      aws.String(ic.instanceArn),
		TargetId:         aws.String(accountID),
		TargetType:       aws.String("AWS_ACCOUNT"),
		PermissionSetArn: aws.String(permissionSetArn),
		PrincipalType:    aws.String("USER"),
		PrincipalId:      aws.String(userID),
	}

	result, err := ic.ssoAdminClient.DeleteAccountAssignmentWithContext(ctx, input)
	if err != nil {
		return err
	}

	// Wait for the deletion to complete
	if result.AccountAssignmentDeletionStatus != nil && result.AccountAssignmentDeletionStatus.RequestId != nil {
		return ic.waitForDeletionCompletion(*result.AccountAssignmentDeletionStatus.RequestId)
	}

	return nil
}

// waitForDeletionCompletion waits for the deletion operation to complete
func (ic *IdentityCenterConfig) waitForDeletionCompletion(requestID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("Waiting for deletion operation to complete (RequestID: %s)...\n", requestID)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for deletion completion")
		default:
			input := &ssoadmin.DescribeAccountAssignmentDeletionStatusInput{
				InstanceArn:                        aws.String(ic.instanceArn),
				AccountAssignmentDeletionRequestId: aws.String(requestID),
			}

			result, err := ic.ssoAdminClient.DescribeAccountAssignmentDeletionStatusWithContext(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to check deletion status: %v", err)
			}

			if result.AccountAssignmentDeletionStatus != nil {
				status := *result.AccountAssignmentDeletionStatus.Status
				fmt.Printf("Deletion status: %s\n", status)

				switch status {
				case "SUCCEEDED":
					return nil
				case "FAILED":
					failureReason := "Unknown error"
					if result.AccountAssignmentDeletionStatus.FailureReason != nil {
						failureReason = *result.AccountAssignmentDeletionStatus.FailureReason
					}
					return fmt.Errorf("deletion failed: %s", failureReason)
				case "IN_PROGRESS":
					time.Sleep(2 * time.Second)
					continue
				default:
					return fmt.Errorf("unexpected deletion status: %s", status)
				}
			}
		}
	}
}

// ListUserAssignments lists all account assignments for a specific user
func (ic *IdentityCenterConfig) ListUserAssignments(userID string) ([]AccountAssignment, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var assignments []AccountAssignment

	// We need to list all permission sets first, then check assignments for each
	permissionSetsInput := &ssoadmin.ListPermissionSetsInput{
		InstanceArn: aws.String(ic.instanceArn),
	}

	err := ic.ssoAdminClient.ListPermissionSetsPagesWithContext(ctx, permissionSetsInput, func(page *ssoadmin.ListPermissionSetsOutput, lastPage bool) bool {
		for _, psArn := range page.PermissionSets {
			// For each permission set, list accounts it's provisioned to
			accountsInput := &ssoadmin.ListAccountsForProvisionedPermissionSetInput{
				InstanceArn:      aws.String(ic.instanceArn),
				PermissionSetArn: psArn,
			}

			ic.ssoAdminClient.ListAccountsForProvisionedPermissionSetPagesWithContext(ctx, accountsInput, func(accountsPage *ssoadmin.ListAccountsForProvisionedPermissionSetOutput, lastAccountPage bool) bool {
				for _, accountID := range accountsPage.AccountIds {
					// Check if this user has this permission set on this account
					assignmentsInput := &ssoadmin.ListAccountAssignmentsInput{
						InstanceArn:      aws.String(ic.instanceArn),
						AccountId:        accountID,
						PermissionSetArn: psArn,
					}

					ic.ssoAdminClient.ListAccountAssignmentsPagesWithContext(ctx, assignmentsInput, func(assignmentsPage *ssoadmin.ListAccountAssignmentsOutput, lastAssignmentPage bool) bool {
						for _, assignment := range assignmentsPage.AccountAssignments {
							if assignment.PrincipalType != nil && *assignment.PrincipalType == "USER" &&
								assignment.PrincipalId != nil && *assignment.PrincipalId == userID {

								// Get permission set name
								psName := ""
								describeInput := &ssoadmin.DescribePermissionSetInput{
									InstanceArn:      aws.String(ic.instanceArn),
									PermissionSetArn: psArn,
								}
								if describeResult, err := ic.ssoAdminClient.DescribePermissionSetWithContext(ctx, describeInput); err == nil {
									if describeResult.PermissionSet.Name != nil {
										psName = *describeResult.PermissionSet.Name
									}
								}

								assignments = append(assignments, AccountAssignment{
									AccountID:         *accountID,
									PermissionSetArn:  *psArn,
									PermissionSetName: psName,
									UserID:            userID,
								})
							}
						}
						return !lastAssignmentPage
					})
				}
				return !lastAccountPage
			})
		}
		return !lastPage
	})

	return assignments, err
}

// AccountAssignment represents a user's assignment to an account with a permission set
type AccountAssignment struct {
	AccountID         string
	PermissionSetArn  string
	PermissionSetName string
	UserID            string
}

// Helper function to check if a string is an ARN
func isArn(s string) bool {
	return len(s) > 20 && s[:4] == "arn:"
}
