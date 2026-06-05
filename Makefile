.PHONY: build test test-verbose lint clean release-dry-run

# ── Build ────────────────────────────────────────────────────────────────────

build:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap .
	zip lambda-function.zip bootstrap

# ── Test ─────────────────────────────────────────────────────────────────────
# Environment variables are set here as a reliable fallback.
# a_test_setup_test.go also sets them for direct `go test` runs via IDE.

TEST_ENV := \
	AWS_REGION=us-east-2 \
	DYNAMO_TABLE=test-pim-requests \
	APPROVERS=test-approvers-secret \
	MANAGEMENT_ACCOUNT=000000000000 \
	LOG_LEVEL=info \
	PIM_ROLE=AdministratorAccess \
	SESSION_TIMEOUT=3600 \
	SES_FROM_EMAIL=noreply@example.com \
	SQS_RESPONSE_QUEUE_URL=https://sqs.us-east-2.amazonaws.com/000000000000/PIM-SQS-Response

test:
	$(TEST_ENV) go test ./...

test-verbose:
	$(TEST_ENV) go test -v ./...

# ── Lint ─────────────────────────────────────────────────────────────────────

lint:
	go vet ./...

# ── Release ──────────────────────────────────────────────────────────────────

release-dry-run:
	goreleaser release --snapshot --clean

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	rm -f bootstrap lambda-function.zip
	rm -rf dist/
