package identitycenter

import (
	"os"
	"testing"
)

// ---------------------------------------------------------------------------
// isArn
// ---------------------------------------------------------------------------

func TestIsArn(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid permission set ARN", "arn:aws:sso:::permissionSet/ssoins-1234/ps-abcd", true},
		{"valid IAM role ARN", "arn:aws:iam::123456789012:role/MyRole", true},
		{"valid S3 ARN", "arn:aws:s3:::my-bucket/my-object", true},
		{"plain permission set name", "AdministratorAccess", false},
		{"plain name DataTeam", "DataTeam", false},
		{"empty string", "", false},
		{"starts with arn but too short", "arn:aws:s3", false}, // len <= 20
		{"arn prefix missing colon", "arnaws:iam::123:role/x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isArn(tt.input)
			if got != tt.expected {
				t.Errorf("isArn(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// loadAdminEmails
// ---------------------------------------------------------------------------

func TestLoadAdminEmails(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		expectedLen int
		contains    []string
		missing     []string
	}{
		{
			name:        "empty env var - no protected users",
			envValue:    "",
			expectedLen: 0,
		},
		{
			name:        "single email",
			envValue:    "admin@example.com",
			expectedLen: 1,
			contains:    []string{"admin@example.com"},
		},
		{
			name:        "multiple comma-separated emails",
			envValue:    "admin@example.com,super@example.com",
			expectedLen: 2,
			contains:    []string{"admin@example.com", "super@example.com"},
		},
		{
			name:        "emails with surrounding spaces are trimmed",
			envValue:    " admin@example.com , super@example.com ",
			expectedLen: 2,
			contains:    []string{"admin@example.com", "super@example.com"},
		},
		{
			name:        "emails stored in lowercase",
			envValue:    "Admin@Example.COM",
			expectedLen: 1,
			contains:    []string{"admin@example.com"},
			missing:     []string{"Admin@Example.COM"},
		},
		{
			name:        "empty entries between commas are ignored",
			envValue:    "admin@example.com,,super@example.com",
			expectedLen: 2,
			contains:    []string{"admin@example.com", "super@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("adminUsers", tt.envValue)
			defer os.Unsetenv("adminUsers")

			result := loadAdminEmails()

			if len(result) != tt.expectedLen {
				t.Errorf("loadAdminEmails() len = %d, want %d (got %v)", len(result), tt.expectedLen, result)
			}
			for _, email := range tt.contains {
				if !result[email] {
					t.Errorf("loadAdminEmails() missing expected key %q", email)
				}
			}
			for _, email := range tt.missing {
				if result[email] {
					t.Errorf("loadAdminEmails() should not contain key %q", email)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isAdminUser
// ---------------------------------------------------------------------------

func TestIsAdminUser(t *testing.T) {
	ic := &IdentityCenterConfig{
		adminEmails: map[string]bool{
			"admin@example.com": true,
			"super@example.com": true,
		},
	}

	tests := []struct {
		email    string
		expected bool
	}{
		{"admin@example.com", true},
		{"super@example.com", true},
		{"regular@example.com", false},
		{"Admin@Example.COM", true},  // lookup is case-insensitive
		{"SUPER@EXAMPLE.COM", true},  // lookup is case-insensitive
		{"", false},
		{"unknown@example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := ic.isAdminUser(tt.email)
			if got != tt.expected {
				t.Errorf("isAdminUser(%q) = %v, want %v", tt.email, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateNotAdminUser
// ---------------------------------------------------------------------------

func TestValidateNotAdminUser(t *testing.T) {
	ic := &IdentityCenterConfig{
		adminEmails: map[string]bool{
			"admin@example.com": true,
		},
	}

	tests := []struct {
		email     string
		operation string
		wantErr   bool
	}{
		{"admin@example.com", "assign permission", true},
		{"Admin@Example.COM", "assign permission", true}, // case-insensitive
		{"regular@example.com", "assign permission", false},
		{"", "assign permission", false}, // empty is not in the admin list
		{"admin@example.com", "remove permission", true},
	}
	for _, tt := range tests {
		t.Run(tt.email+"/"+tt.operation, func(t *testing.T) {
			err := ic.validateNotAdminUser(tt.email, tt.operation)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateNotAdminUser(%q, %q) error = %v, wantErr %v",
					tt.email, tt.operation, err, tt.wantErr)
			}
		})
	}
}
