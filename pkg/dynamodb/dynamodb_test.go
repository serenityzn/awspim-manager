package dynamodb

import (
	"testing"
)

// ---------------------------------------------------------------------------
// RequestStatus.String()
// ---------------------------------------------------------------------------

func TestRequestStatusString(t *testing.T) {
	tests := []struct {
		status   RequestStatus
		expected string
	}{
		{Pending, "Pending"},
		{Approved, "Approved"},
		{Denied, "Denied"},
		{Expired, "Expired"},
		{RequestStatus(0), "Unknown"},
		{RequestStatus(99), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.expected {
				t.Errorf("RequestStatus(%d).String() = %q, want %q", tt.status, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RequestStatus.EnumIndex()
// ---------------------------------------------------------------------------

func TestRequestStatusEnumIndex(t *testing.T) {
	tests := []struct {
		status   RequestStatus
		expected int
	}{
		{Pending, 1},
		{Approved, 2},
		{Denied, 3},
		{Expired, 4},
	}
	for _, tt := range tests {
		t.Run(tt.status.String(), func(t *testing.T) {
			got := tt.status.EnumIndex()
			if got != tt.expected {
				t.Errorf("RequestStatus(%d).EnumIndex() = %d, want %d", tt.status, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseRequestStatus
// ---------------------------------------------------------------------------

func TestParseRequestStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected RequestStatus
	}{
		{"Pending", Pending},
		{"Approved", Approved},
		{"Denied", Denied},
		{"Expired", Expired},
		// anything unrecognised falls back to Pending (current behaviour)
		{"unknown", Pending},
		{"", Pending},
		{"APPROVED", Pending}, // case-sensitive
		{"expired", Pending},  // case-sensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseRequestStatus(tt.input)
			if got != tt.expected {
				t.Errorf("ParseRequestStatus(%q) = %v (%d), want %v (%d)",
					tt.input, got, got, tt.expected, tt.expected)
			}
		})
	}
}

// ParseRequestStatus and String() should be inverse operations for all known statuses.
func TestParseRequestStatusRoundTrip(t *testing.T) {
	statuses := []RequestStatus{Pending, Approved, Denied, Expired}
	for _, s := range statuses {
		t.Run(s.String(), func(t *testing.T) {
			got := ParseRequestStatus(s.String())
			if got != s {
				t.Errorf("round-trip failed: ParseRequestStatus(%q) = %v, want %v", s.String(), got, s)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Timestamp.String()
// ---------------------------------------------------------------------------

func TestTimestampString(t *testing.T) {
	tests := []struct {
		ts       Timestamp
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{1700000000, "1700000000"},
		{-1, "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.ts.String()
			if got != tt.expected {
				t.Errorf("Timestamp(%d).String() = %q, want %q", tt.ts, got, tt.expected)
			}
		})
	}
}
