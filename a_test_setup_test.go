package main

import "os"

// init sets the environment variables required by main.go's init() before it
// runs. Go processes files within a package alphabetically, so this file
// (prefix "a") is initialised before main.go (prefix "m"), ensuring the vars
// are present when main.go's init() reads them.
//
// Only sets a variable if it has not already been provided externally, so real
// values injected via the shell or CI will always take precedence.
func init() {
	defaults := map[string]string{
		"AWS_REGION":         "us-east-2",
		"DYNAMO_TABLE":       "test-pim-requests",
		"APPROVERS":          "test-approvers-secret",
		"MANAGEMENT_ACCOUNT": "000000000000",
		"LOG_LEVEL":          "info",
		"PIM_ROLE":           "AdministratorAccess",
		"SESSION_TIMEOUT":    "3600",
	}
	for k, v := range defaults {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
