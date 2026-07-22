.PHONY: build test fmt check-fmt lint vet check docker assert-demo

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

# Optional messaging smoke (not part of default check).
assert-demo:
	./scripts/assert-demo.sh

# Build a local Docker image for Linux-native ntnbox run (ntnbox:latest).
# Published multi-arch images: ghcr.io/hyavari/ntn-in-a-box:{version,latest}
docker:
	docker build -t ntnbox:latest .
