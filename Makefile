# hostbud — every tool runs in a container; the host only needs Docker.
# Caches live in ./.cache (gitignored) so containers can run as your user.

GO_IMAGE       ?= golang:1.27.1-bookworm
LINT_IMAGE     ?= golangci/golangci-lint:v2.14.0
GITLEAKS_IMAGE ?= zricethezav/gitleaks:v8.30.1

UID := $(shell id -u)
GID := $(shell id -g)

DOCKER_GO = docker run --rm -u $(UID):$(GID) -v "$(CURDIR)":/src -w /src \
	-e HOME=/tmp -e GOFLAGS=-buildvcs=false \
	-e GOMODCACHE=/src/.cache/gomod -e GOCACHE=/src/.cache/gobuild \
	-e GOLANGCI_LINT_CACHE=/src/.cache/golangci
GITLEAKS = docker run --rm -v "$(CURDIR)":/repo $(GITLEAKS_IMAGE)

.PHONY: help build test lint fmt tidy gitleaks gitleaks-staged hooks

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

build: ## Build the hostbud binary into ./bin
	$(DOCKER_GO) -e CGO_ENABLED=0 $(GO_IMAGE) go build -o bin/hostbud ./cmd/hostbud

test: ## Run unit tests
	$(DOCKER_GO) $(GO_IMAGE) go test -race ./...

lint: ## Run golangci-lint (linters + formatting check)
	$(DOCKER_GO) $(LINT_IMAGE) golangci-lint run ./...

fmt: ## Format Go code
	$(DOCKER_GO) $(LINT_IMAGE) golangci-lint fmt ./...

tidy: ## go mod tidy
	$(DOCKER_GO) $(GO_IMAGE) go mod tidy

gitleaks: ## Scan the whole git history for secrets
	$(GITLEAKS) git /repo --no-banner --redact

gitleaks-staged: ## Scan staged changes for secrets (used by the pre-commit hook)
	@$(GITLEAKS) git /repo --no-banner --redact --staged

hooks: ## Install the repo's git hooks (gitleaks pre-commit)
	git config core.hooksPath .githooks
	@echo "hooks installed from .githooks/"
