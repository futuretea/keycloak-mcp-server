BINARY_NAME ?= keycloak
GOLANGCI_LINT ?= golangci-lint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/futuretea/keycloak-mcp-server/pkg/core/version.Version=$(VERSION) \
	-X github.com/futuretea/keycloak-mcp-server/pkg/core/version.Commit=$(COMMIT) \
	-X github.com/futuretea/keycloak-mcp-server/pkg/core/version.Date=$(DATE)

-include release.mk

.PHONY: build test e2e test-template-init lint format tidy ci clean docker coverage smoke print-binary-name

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/mcp-server

smoke: build
	./bin/$(BINARY_NAME) version

print-binary-name:
	@printf '%s\n' "$(BINARY_NAME)"

test:
	go test ./...

e2e:
	go test -tags=e2e ./test/e2e

test-template-init:
	./scripts/test-init-template.sh

lint:
	go vet ./...
	test -z "$$(gofmt -l cmd internal pkg)"
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run ./...; \
	else \
		echo "$(GOLANGCI_LINT) not installed, skipping"; \
	fi

format:
	gofmt -w $$(find cmd internal pkg -name '*.go')

tidy:
	go mod tidy

coverage:
	go test ./pkg/... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

ci: lint test e2e test-template-init build

DOCKER_TAG ?= dev

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(DATE) \
		-t ghcr.io/futuretea/keycloak-mcp-server:$(DOCKER_TAG) \
		-t ghcr.io/futuretea/keycloak-mcp-server:$(VERSION) .

clean:
	rm -rf bin coverage.out coverage.txt
