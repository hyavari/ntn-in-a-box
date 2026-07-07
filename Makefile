.PHONY: build test fmt lint vet check docker

build:
	go build ./...

test:
	go test ./...

fmt:
	gofmt -l -w .
	goimports -l -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

# Run everything CI would run.
check: fmt vet lint test build

# Build the Docker image for Linux-native ntnbox run.
docker:
	docker build -t ntnbox:latest .
