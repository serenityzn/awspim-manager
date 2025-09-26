# Identity Center Package

This package provides a comprehensive wrapper for AWS Identity Center (formerly AWS SSO) operations, specifically designed for Privileged Identity Management (PIM) systems. It enables automated user management, permission assignment, and access control through AWS Identity Center.

## Overview

The Identity Center package manages user access to AWS accounts through permission sets. It provides methods for listing users, assigning permissions, removing access, and protecting admin users from unauthorized modifications.

## Data Structures

### User Information

```go
type User struct {
    UserID      string  // Unique UUID identifier from Identity Store
    UserName    string  // Username for the user
    DisplayName string  // Full display name
    Email       string  // Primary email address
    FirstName   string  // Given name
    LastName    string  // Family name
    Active      bool    // Whether user is active (always true for returned users)
    UserState   string  // ACTIVE, INACTIVE, etc.
}
```

### Account Assignment

```go
type AccountAssignment struct {
    AccountID         string  // AWS Account ID
    PermissionSetArn  string  // Full ARN of the permission set
    PermissionSetName string  // Human-readable name of permission set
    UserID           string   // User's unique identifier
}
```

### Configuration

```go
type IdentityCenterConfig struct {
    ssoAdminClient      *ssoadmin.SSOAdmin         // SSO Admin service client
    identityStoreClient *identitystore.IdentityStore // Identity Store service client
    organizationsClient *organizations.Organizations // Organizations service client
    region              string                      // AWS region
    instanceArn         string                      // SSO instance ARN
    identityStoreId     string                      // Identity Store ID
    adminEmails         map[string]bool             // Protected admin emails
}
```

## Key Features

### 🔐 Admin User Protection
- **Environment variable configuration** for protected admin users
- **Automatic blocking** of assign/remove operations on admin users
- **Case-insensitive email matching** for robust protection

### 👥 User Management
- **List all users** or filter by enabled status
- **Find users by email or username** with automatic User ID lookup
- **Comprehensive user information** extraction

### 🎯 Permission Assignment
- **Assign users to accounts** with specific permission sets
- **Support for both ARN and name** permission set identifiers
- **Duplicate prevention** - checks existing assignments
- **Real-time progress monitoring** with status updates

### 🗑️ Permission Removal
- **Remove specific permission sets** from users
- **Smart validation** - only removes if assignment exists
- **Graceful handling** of non-existent assignments

### ✅ Comprehensive Validation
- **Account existence** verification through AWS Organizations
- **User existence** validation in Identity Store
- **Permission set validation** and name-to-ARN resolution

## Environment Configuration

### Admin User Protection

Set the `adminUsers` environment variable with comma-separated admin emails:

```bash
export adminUsers="admin@company.com,superuser@company.com,root@company.com"
```

**Features:**
- Case-insensitive matching
- Automatic whitespace trimming
- Startup logging of protected users
- Blocks both assign and remove operations

## Usage

### 1. Initialize Configuration

```go
import "pim-manager/pkg/identitycenter"

// Basic configuration
ic, err := identitycenter.NewIdentityCenterConfig("us-east-2")
if err != nil {
    log.Fatal(err)
}

// Output during initialization:
// Protected admin user: admin@company.com
// Protected admin user: superuser@company.com
// Loaded 2 admin users for protection
```

### 2. List Users

```go
// Get all users (regardless of status)
allUsers, err := ic.ListUsers()
if err != nil {
    log.Printf("Failed to list users: %v", err)
}

// Get only enabled/active users
enabledUsers, err := ic.ListEnabledUsers()
if err != nil {
    log.Printf("Failed to list enabled users: %v", err)
}

// Custom filter (ACTIVE, INACTIVE, etc.)
activeUsers, err := ic.ListUsersWithFilter("ACTIVE")
if err != nil {
    log.Printf("Failed to list active users: %v", err)
}

// Display user information
for _, user := range enabledUsers {
    fmt.Printf("User: %s (%s) - %s - State: %s\n", 
        user.DisplayName, user.UserName, user.Email, user.UserState)
}
```

### 3. Find Specific Users

```go
// Find user by email address
userID, err := ic.FindUserByEmail("john.doe@company.com")
if err != nil {
    log.Printf("User not found: %v", err)
} else {
    fmt.Printf("Found user ID: %s\n", userID)
}

// Find user by username
userID, err := ic.FindUserByUsername("john.doe")
if err != nil {
    log.Printf("User not found: %v", err)
}

// Get detailed user information
user, err := ic.GetUser(userID)
if err != nil {
    log.Printf("Failed to get user details: %v", err)
} else {
    fmt.Printf("User Details: %+v\n", user)
}
```

### 4. Permission Assignment

#### Assign by Email (Recommended)

```go
// Assign permission set to user by email
err := ic.AssignUserToAccountByEmail(
    "john.doe@company.com",  // User email
    "123456789012",          // AWS Account ID
    "ReadOnlyAccess"         // Permission Set Name
)
if err != nil {
    log.Printf("Assignment failed: %v", err)
}
```

#### Assign by Username

```go
err := ic.AssignUserToAccountByUsername(
    "john.doe",              // Username
    "123456789012",          // AWS Account ID
    "ReadOnlyAccess"         // Permission Set Name
)
```

#### Assign by User ID (Direct)

```go
err := ic.AssignUserToAccount(
    "12345678-1234-1234-1234-123456789012", // User ID (UUID)
    "123456789012",                         // AWS Account ID
    "arn:aws:sso:::permissionSet/..."       // Permission Set ARN
)
```

#### Assignment Process Output

```
Finding user by email: john.doe@company.com...
Found user ID: 12345678-abcd-1234-efgh-567890123456 for email: john.doe@company.com
Validating account 123456789012...
Validating user 12345678-abcd-1234-efgh-567890123456...
Finding permission set by name 'ReadOnlyAccess'...
Checking if assignment already exists...
Creating assignment...
Waiting for assignment operation to complete (RequestID: req-abc123)...
Assignment status: IN_PROGRESS
Assignment status: SUCCEEDED
Successfully assigned user 12345678-abcd-1234-efgh-567890123456 to account 123456789012 with permission set arn:aws:sso:::permissionSet/...
```

### 5. Permission Removal

#### Remove by Email

```go
// Remove specific permission set from user
err := ic.RemoveUserFromAccountByEmail(
    "john.doe@company.com",  // User email
    "123456789012",          // AWS Account ID
    "ReadOnlyAccess"         // Permission Set Name
)
if err != nil {
    log.Printf("Removal failed: %v", err)
}
```

#### Remove by Username

```go
err := ic.RemoveUserFromAccountByUsername(
    "john.doe",              // Username
    "123456789012",          // AWS Account ID
    "ReadOnlyAccess"         // Permission Set Name
)
```

#### Remove by User ID

```go
err := ic.RemoveUserFromAccount(
    "12345678-1234-1234-1234-123456789012", // User ID
    "123456789012",                         // AWS Account ID
    "ReadOnlyAccess"                        // Permission Set Name or ARN
)
```

#### Smart Removal Logic

```
Finding user by email: john.doe@company.com...
Found user ID: 12345678-abcd-1234-efgh-567890123456 for email: john.doe@company.com
Validating user 12345678-abcd-1234-efgh-567890123456...
Finding permission set by name 'ReadOnlyAccess'...
Checking if assignment exists for user 12345678-abcd-1234-efgh-567890123456...
Removing assignment...
Waiting for deletion operation to complete (RequestID: req-xyz789)...
Deletion status: SUCCEEDED
Successfully removed user 12345678-abcd-1234-efgh-567890123456 from account 123456789012
```

### 6. List User Assignments

```go
// Get all account assignments for a user
assignments, err := ic.ListUserAssignments(userID)
if err != nil {
    log.Printf("Failed to list assignments: %v", err)
} else {
    fmt.Printf("User has %d assignments:\n", len(assignments))
    for _, assignment := range assignments {
        fmt.Printf("- Account: %s, Permission Set: %s (%s)\n", 
            assignment.AccountID, 
            assignment.PermissionSetName, 
            assignment.PermissionSetArn)
    }
}
```

### 7. Permission Set Management

```go
// Find permission set ARN by name
permissionSetArn, err := ic.FindPermissionSetByName("ReadOnlyAccess")
if err != nil {
    log.Printf("Permission set not found: %v", err)
} else {
    fmt.Printf("Permission Set ARN: %s\n", permissionSetArn)
}

// Validate permission set exists
err = ic.ValidatePermissionSet("arn:aws:sso:::permissionSet/...")
if err != nil {
    log.Printf("Permission set validation failed: %v", err)
}
```

## Methods Reference

### Configuration Methods

#### `NewIdentityCenterConfig(region string) (*IdentityCenterConfig, error)`
Creates a new Identity Center configuration with automatic SSO instance discovery and admin user protection setup.

### User Discovery Methods

#### `ListUsers() ([]User, error)`
Returns all users from Identity Center regardless of status.

#### `ListEnabledUsers() ([]User, error)`
Returns only users with UserState = "ACTIVE".

#### `ListUsersWithFilter(userState string) ([]User, error)`
Returns users filtered by specified UserState ("ACTIVE", "INACTIVE", etc.).

#### `FindUserByEmail(email string) (string, error)`
Finds a user by email address and returns their User ID.

#### `FindUserByUsername(username string) (string, error)`
Finds a user by username and returns their User ID.

#### `GetUser(userID string) (*User, error)`
Retrieves detailed information for a specific user.

### Assignment Methods

#### `AssignUserToAccountByEmail(email, accountID, permissionSetIdentifier string) error`
Assigns a permission set to a user using their email address. Most user-friendly method.

#### `AssignUserToAccountByUsername(username, accountID, permissionSetIdentifier string) error`
Assigns a permission set to a user using their username.

#### `AssignUserToAccount(userID, accountID, permissionSetIdentifier string) error`
Assigns a permission set to a user using their User ID (UUID format).

### Removal Methods

#### `RemoveUserFromAccountByEmail(email, accountID, permissionSetIdentifier string) error`
Removes a permission set from a user using their email address.

#### `RemoveUserFromAccountByUsername(username, accountID, permissionSetIdentifier string) error`
Removes a permission set from a user using their username.

#### `RemoveUserFromAccount(userID, accountID, permissionSetIdentifier string) error`
Removes a permission set from a user using their User ID.

### Validation Methods

#### `ValidateAccount(accountID string) error`
Verifies that an AWS account exists in the organization.

#### `ValidateUser(userID string) error`
Verifies that a user exists in Identity Store.

#### `ValidatePermissionSet(permissionSetArn string) error`
Verifies that a permission set exists.

### Utility Methods

#### `ListUserAssignments(userID string) ([]AccountAssignment, error)`
Lists all account assignments for a specific user.

#### `FindPermissionSetByName(permissionSetName string) (string, error)`
Finds a permission set ARN by its name.

#### `GetIdentityStoreID() (string, error)`
Returns the Identity Store ID for the SSO instance.

## Admin User Protection

### How It Works

1. **Environment Variable**: Set `adminUsers` with comma-separated emails
2. **Automatic Loading**: Admin emails loaded during configuration initialization
3. **Operation Blocking**: All assign/remove operations check admin status
4. **Clear Feedback**: Operations blocked with descriptive error messages

### Protection Examples

#### Successful Protection

```go
// Environment: adminUsers="admin@company.com,superuser@company.com"

err := ic.AssignUserToAccountByEmail("admin@company.com", "123456789012", "DataTeam")
// Output: ⚠️  operation 'assign permission' blocked: user admin@company.com is a protected admin user
// Returns error, operation blocked
```

#### Normal Operation

```go
err := ic.AssignUserToAccountByEmail("regular.user@company.com", "123456789012", "DataTeam")
// Proceeds normally with full assignment process
```

### Case-Insensitive Protection

```bash
# Environment Variable
export adminUsers="Admin@Company.com, SUPER@COMPANY.COM"

# All these variations are protected:
# admin@company.com     ✅ Protected
# Admin@Company.COM     ✅ Protected  
# SUPER@company.com     ✅ Protected
```

## Error Handling

### Common Error Scenarios

#### User Not Found
```go
err := ic.FindUserByEmail("nonexistent@company.com")
// Error: user with email 'nonexistent@company.com' not found
```

#### Permission Set Not Found
```go
err := ic.AssignUserToAccountByEmail("user@company.com", "123456789012", "NonExistentRole")
// Error: permission set lookup failed: permission set with name 'NonExistentRole' not found
```

#### Admin User Protection
```go
err := ic.AssignUserToAccountByEmail("admin@company.com", "123456789012", "DataTeam")
// Error: operation 'assign permission' blocked: user admin@company.com is a protected admin user
```

#### Assignment Already Exists
```go
err := ic.AssignUserToAccountByEmail("user@company.com", "123456789012", "ReadOnlyAccess")
// Output: Assignment already exists for user ... Nothing to do.
// Returns: nil (no error)
```

#### No Assignment to Remove
```go
err := ic.RemoveUserFromAccountByEmail("user@company.com", "123456789012", "NonAssignedRole")
// Output: No assignment found for user ... Nothing to remove.
// Returns: nil (no error)
```

## Complete Workflow Examples

### 1. PIM Request Approval Workflow

```go
package main

import (
    "fmt"
    "log"
    "pim-manager/pkg/identitycenter"
)

func approvePIMRequest(requesterEmail, accountID, permissionSetName string) error {
    // Initialize Identity Center
    ic, err := identitycenter.NewIdentityCenterConfig("us-east-2")
    if err != nil {
        return err
    }

    // Assign permission to user
    fmt.Printf("Approving access for %s to account %s\n", requesterEmail, accountID)
    err = ic.AssignUserToAccountByEmail(requesterEmail, accountID, permissionSetName)
    if err != nil {
        return fmt.Errorf("failed to assign permission: %v", err)
    }

    fmt.Println("✅ Access granted successfully")
    return nil
}

func main() {
    err := approvePIMRequest("john.doe@company.com", "123456789012", "ReadOnlyAccess")
    if err != nil {
        log.Fatal(err)
    }
}
```

### 2. Access Revocation Workflow

```go
func revokePIMAccess(userEmail, accountID, permissionSetName string) error {
    ic, err := identitycenter.NewIdentityCenterConfig("us-east-2")
    if err != nil {
        return err
    }

    fmt.Printf("Revoking access for %s from account %s\n", userEmail, accountID)
    err = ic.RemoveUserFromAccountByEmail(userEmail, accountID, permissionSetName)
    if err != nil {
        return fmt.Errorf("failed to remove permission: %v", err)
    }

    fmt.Println("✅ Access revoked successfully")
    return nil
}
```

### 3. User Audit Workflow

```go
func auditUserAccess(userEmail string) error {
    ic, err := identitycenter.NewIdentityCenterConfig("us-east-2")
    if err != nil {
        return err
    }

    // Find user
    userID, err := ic.FindUserByEmail(userEmail)
    if err != nil {
        return fmt.Errorf("user not found: %v", err)
    }

    // Get user details
    user, err := ic.GetUser(userID)
    if err != nil {
        return fmt.Errorf("failed to get user details: %v", err)
    }

    fmt.Printf("User: %s (%s)\n", user.DisplayName, user.Email)
    fmt.Printf("Status: %s\n", user.UserState)

    // List assignments
    assignments, err := ic.ListUserAssignments(userID)
    if err != nil {
        return fmt.Errorf("failed to list assignments: %v", err)
    }

    fmt.Printf("Current Assignments (%d):\n", len(assignments))
    for _, assignment := range assignments {
        fmt.Printf("  - Account: %s, Role: %s\n", 
            assignment.AccountID, assignment.PermissionSetName)
    }

    return nil
}
```

### 4. Bulk User Management

```go
func bulkAssignPermissions(userEmails []string, accountID, permissionSetName string) {
    ic, err := identitycenter.NewIdentityCenterConfig("us-east-2")
    if err != nil {
        log.Fatal(err)
    }

    successCount := 0
    errorCount := 0

    for _, email := range userEmails {
        fmt.Printf("Processing user: %s\n", email)
        
        err := ic.AssignUserToAccountByEmail(email, accountID, permissionSetName)
        if err != nil {
            fmt.Printf("  ❌ Failed: %v\n", err)
            errorCount++
        } else {
            fmt.Printf("  ✅ Success\n")
            successCount++
        }
    }

    fmt.Printf("\nBulk assignment complete: %d success, %d errors\n", successCount, errorCount)
}
```

## Performance Considerations

### Efficient User Lookups
- **Cache user lists** when making multiple lookups
- **Use direct User IDs** when available to avoid email/username lookups
- **Batch operations** when processing multiple users

### Rate Limiting
- AWS Identity Center has API rate limits
- **Implement delays** between bulk operations
- **Monitor CloudTrail** for throttling events

### Connection Reuse
- Single configuration instance for multiple operations
- Automatic session management through AWS SDK
- Cached SSO instance details for efficiency

## Best Practices

### 1. User Identification
```go
// Preferred: Use email addresses (most user-friendly)
err := ic.AssignUserToAccountByEmail("user@company.com", accountID, roleName)

// Alternative: Use username when email not available
err := ic.AssignUserToAccountByUsername("username", accountID, roleName)

// Direct: Use User ID when performance is critical
err := ic.AssignUserToAccount(userID, accountID, roleArn)
```

### 2. Permission Set References
```go
// Preferred: Use human-readable names
permissionSet := "ReadOnlyAccess"

// Alternative: Use ARNs for exact matching
permissionSet := "arn:aws:sso:::permissionSet/ins-123/ps-456"
```

### 3. Error Handling
```go
err := ic.AssignUserToAccountByEmail(email, accountID, roleName)
if err != nil {
    // Check for specific error types
    if strings.Contains(err.Error(), "protected admin user") {
        log.Printf("Skipping admin user: %s", email)
        return nil // Don't treat as failure
    }
    if strings.Contains(err.Error(), "already exists") {
        log.Printf("Assignment already exists for: %s", email)
        return nil // Not an error condition
    }
    return fmt.Errorf("assignment failed: %v", err)
}
```

### 4. Admin Protection Setup
```bash
# Set environment variable before starting application
export adminUsers="admin@company.com,superuser@company.com,security@company.com"

# Verify protection is active by checking startup logs:
# Protected admin user: admin@company.com
# Protected admin user: superuser@company.com
# Protected admin user: security@company.com
# Loaded 3 admin users for protection
```

### 5. Validation Before Operations
```go
// Validate components before assignment
err := ic.ValidateAccount(accountID)
if err != nil {
    return fmt.Errorf("invalid account: %v", err)
}

userID, err := ic.FindUserByEmail(email)
if err != nil {
    return fmt.Errorf("user not found: %v", err)
}

permissionSetArn, err := ic.FindPermissionSetByName(permissionSetName)
if err != nil {
    return fmt.Errorf("permission set not found: %v", err)
}

// Proceed with assignment
err = ic.AssignUserToAccount(userID, accountID, permissionSetArn)
```

## Dependencies

- **AWS SDK for Go v1**: `github.com/aws/aws-sdk-go`
  - SSO Admin service
  - Identity Store service
  - Organizations service

## Configuration Requirements

### AWS Credentials
- AWS credentials configured via environment variables, credentials file, or IAM role
- Appropriate IAM permissions for Identity Center operations

### Environment Variables
- `adminUsers` (optional): Comma-separated list of protected admin emails

### AWS Services
- AWS Identity Center enabled and configured
- AWS Organizations (for account validation)
- Identity Store with users configured

## IAM Permissions Required

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "sso:ListInstances",
                "sso:DescribePermissionSet",
                "sso:ListPermissionSets",
                "sso:CreateAccountAssignment",
                "sso:DeleteAccountAssignment",
                "sso:ListAccountAssignments",
                "sso:ListAccountsForProvisionedPermissionSet",
                "sso:DescribeAccountAssignmentCreationStatus",
                "sso:DescribeAccountAssignmentDeletionStatus"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": [
                "identitystore:DescribeUser",
                "identitystore:ListUsers"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": [
                "organizations:DescribeAccount",
                "organizations:ListAccounts",
                "organizations:DescribeOrganization"
            ],
            "Resource": "*"
        }
    ]
}
```

## Security Considerations

### Admin User Protection
- **Environment-based configuration** prevents hardcoded admin lists
- **Case-insensitive matching** prevents bypass attempts
- **Operation blocking** at the API level, not just UI level

### Audit Trail
- All operations generate CloudTrail events
- Detailed logging with request IDs for tracking
- Clear error messages for failed operations

### Principle of Least Privilege
- Validate all inputs before operations
- Check existing assignments to prevent duplicates
- Fail safely when operations cannot be completed

### Rate Limiting
- Implement appropriate delays for bulk operations
- Monitor AWS service limits and quotas
- Handle throttling gracefully with retries

This package provides a complete solution for managing AWS Identity Center permissions programmatically while maintaining security and operational best practices.
