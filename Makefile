.PHONY: build test fmt lint vet check

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
