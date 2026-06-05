package main

import "os"

// init sets environment variable defaults used by production code.
// Go always runs non-test file init() before test file init(), so
// main.go's init() reads env vars before this runs. Required-var validation
// is therefore deferred to handler invocation (via configError), where these
// defaults are already in place.
//
// Only sets a variable if it has not already been provided externally, so real
// values injected via the shell or CI will always take precedence.
func init() {
	defaults := map[string]string{
		"AWS_REGION":            "us-east-2",
		"DYNAMO_TABLE":          "test-pim-requests",
		"APPROVERS":             "test-approvers-secret",
		"MANAGEMENT_ACCOUNT":    "000000000000",
		"LOG_LEVEL":             "info",
		"PIM_ROLE":              "AdministratorAccess",
		"SESSION_TIMEOUT":       "3600",
		"SES_FROM_EMAIL":        "noreply@example.com",
		"SQS_RESPONSE_QUEUE_URL": "https://sqs.us-east-2.amazonaws.com/000000000000/PIM-SQS-Response",
	}
	for k, v := range defaults {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
