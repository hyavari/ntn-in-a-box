.PHONY: build test fmt check-fmt lint vet check docker assert-demo hooks

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

# On macOS, also lint with GOOS=linux so Darwin-only call sites don't hide
# unused symbols the way Linux CI would see them.
lint:
	golangci-lint run ./...
ifneq ($(shell go env GOOS),linux)
	GOOS=linux golangci-lint run ./...
endif

# Run everything CI would run (non-mutating).
check: check-fmt vet lint test build

# Point this clone at .githooks/ (pre-commit runs check-fmt + lint).
hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "git hooks enabled (core.hooksPath=.githooks)"

# Optional messaging smoke (not part of default check).
assert-demo:
	./scripts/assert-demo.sh

# Build a local Docker image for Linux-native ntnbox run (ntnbox:latest).
# Published multi-arch images: ghcr.io/hyavari/ntn-in-a-box:{version,latest}
docker:
	docker build -t ntnbox:latest .
