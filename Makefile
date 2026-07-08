.PHONY: build test fmt check-fmt lint vet check docker

build:
	go build ./...

test:
	go test ./...

fmt:
	gofmt -l -w .
	goimports -l -w .

# Verify formatting without mutating (for CI/check).
check-fmt:
	@test -z "$$(gofmt -l .)" || (echo "gofmt: files need formatting:" && gofmt -l . && exit 1)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Run everything CI would run (non-mutating).
check: check-fmt vet lint test build

# Build the Docker image for Linux-native ntnbox run.
docker:
	docker build -t ntnbox:latest .
