package main

import (
	"os"
	"testing"
)

// TestMain is the entry point for the test binary.
// Environment variables are set in a_test_setup_test.go before init() runs.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// formatDuration
// ---------------------------------------------------------------------------

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		seconds  int64
		expected string
	}{
		{"zero", 0, "0 seconds"},
		{"one second", 1, "1 seconds"},
		{"59 seconds", 59, "59 seconds"},
		{"exactly 1 minute", 60, "1 minutes"},
		{"90 seconds", 90, "1 minutes 30 seconds"},
		{"exactly 1 hour", 3600, "1 hours"},
		{"1 hour 30 minutes", 5400, "1 hours 30 minutes"},
		{"2 hours exactly", 7200, "2 hours"},
		{"2 hours 15 minutes", 8100, "2 hours 15 minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.seconds)
			if got != tt.expected {
				t.Errorf("formatDuration(%d) = %q, want %q", tt.seconds, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isDebug
// ---------------------------------------------------------------------------

func TestIsDebug(t *testing.T) {
	tests := []struct {
		level    string
		expected bool
	}{
		{"debug", true},
		{"DEBUG", true},
		{"Debug", true},
		{"info", false},
		{"INFO", false},
		{"warn", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			orig := LogLevel
			LogLevel = tt.level
			defer func() { LogLevel = orig }()

			got := isDebug()
			if got != tt.expected {
				t.Errorf("isDebug() with LogLevel=%q = %v, want %v", tt.level, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateEmail
// ---------------------------------------------------------------------------

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
	}{
		{"valid simple", "user@example.com", false},
		{"valid subdomain", "user@mail.example.com", false},
		{"valid plus addressing", "user+tag@example.com", false},
		{"valid corporate", "john.doe@company.co.uk", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"missing @", "notanemail", true},
		{"missing domain", "user@", true},
		{"missing local part", "@example.com", true},
		{"double @", "user@@example.com", true},
		{"missing TLD", "user@example", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmail("field", tt.email)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEmail(%q) error = %v, wantErr %v", tt.email, err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateSQSMessage
// ---------------------------------------------------------------------------

func TestValidateSQSMessage(t *testing.T) {
	valid := SQSMessage{
		Requestor: "user@example.com",
		Approver:  "manager@example.com",
		Account:   "123456789012",
	}

	tests := []struct {
		name    string
		msg     SQSMessage
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid message",
			msg:     valid,
			wantErr: false,
		},
		// requestor
		{
			name:    "empty requestor",
			msg:     SQSMessage{Requestor: "", Approver: valid.Approver, Account: valid.Account},
			wantErr: true,
			errMsg:  "requestor is empty",
		},
		{
			name:    "invalid requestor - no @",
			msg:     SQSMessage{Requestor: "notanemail", Approver: valid.Approver, Account: valid.Account},
			wantErr: true,
		},
		{
			name:    "invalid requestor - no domain",
			msg:     SQSMessage{Requestor: "user@", Approver: valid.Approver, Account: valid.Account},
			wantErr: true,
		},
		// approver
		{
			name:    "empty approver",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: "", Account: valid.Account},
			wantErr: true,
			errMsg:  "approver is empty",
		},
		{
			name:    "invalid approver - no @",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: "notanemail", Account: valid.Account},
			wantErr: true,
		},
		// account
		{
			name:    "empty account",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: ""},
			wantErr: true,
			errMsg:  "account is empty",
		},
		{
			name:    "account too short - 11 digits",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: "12345678901"},
			wantErr: true,
		},
		{
			name:    "account too long - 13 digits",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: "1234567890123"},
			wantErr: true,
		},
		{
			name:    "account contains letters",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: "12345678901a"},
			wantErr: true,
		},
		{
			name:    "account contains spaces",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: "123456 789012"},
			wantErr: true,
		},
		{
			name:    "account contains hyphens",
			msg:     SQSMessage{Requestor: valid.Requestor, Approver: valid.Approver, Account: "123-456-7890"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSQSMessage(tt.msg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSQSMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errMsg != "" && err != nil && err.Error() != tt.errMsg {
				t.Errorf("validateSQSMessage() error = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validateNotManagementAccount
// ---------------------------------------------------------------------------

func TestValidateNotManagementAccount(t *testing.T) {
	tests := []struct {
		name       string
		account    string
		management string
		wantErr    bool
	}{
		{"different accounts - allowed", "111122223333", "000000000000", false},
		{"same account - blocked", "000000000000", "000000000000", true},
		{"account has leading space - blocked", " 000000000000", "000000000000", true},
		{"management has trailing space - blocked", "000000000000", "000000000000 ", true},
		{"both have spaces - blocked", " 000000000000 ", " 000000000000 ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNotManagementAccount(tt.account, tt.management)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateNotManagementAccount(%q, %q) error = %v, wantErr %v",
					tt.account, tt.management, err, tt.wantErr)
			}
		})
	}
}
