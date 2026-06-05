.PHONY: build test test-verbose lint clean

# ── Build ────────────────────────────────────────────────────────────────────

build:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o pim .

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
	SESSION_TIMEOUT=3600

test:
	$(TEST_ENV) go test ./...

test-verbose:
	$(TEST_ENV) go test -v ./...

# ── Lint ─────────────────────────────────────────────────────────────────────

lint:
	go vet ./...

# ── Clean ────────────────────────────────────────────────────────────────────

clean:
	rm -f pim
