# hostbud — every tool runs in a container; the host only needs Docker.
# Caches live in ./.cache (gitignored) so containers can run as your user.

GO_IMAGE       ?= golang:1.27.1-bookworm
LINT_IMAGE     ?= golangci/golangci-lint:v2.14.0
GITLEAKS_IMAGE ?= zricethezav/gitleaks:v8.30.1
NODE_IMAGE     ?= node:24.21.0-bookworm-slim

UID := $(shell id -u)
GID := $(shell id -g)

DOCKER_GO = docker run --rm -u $(UID):$(GID) -v "$(CURDIR)":/src -w /src \
	-e HOME=/tmp -e GOFLAGS=-buildvcs=false \
	-e GOMODCACHE=/src/.cache/gomod -e GOCACHE=/src/.cache/gobuild \
	-e GOLANGCI_LINT_CACHE=/src/.cache/golangci
# pnpm comes from corepack (version pinned by web/package.json "packageManager").
DOCKER_PNPM = docker run --rm -u $(UID):$(GID) -v "$(CURDIR)":/src -w /src/web \
	-e HOME=/tmp -e CI=true -e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
	-e COREPACK_HOME=/src/.cache/corepack -e pnpm_config_store_dir=/src/.cache/pnpm-store \
	$(NODE_IMAGE) corepack pnpm
DOCKER_PNPM_E2E = docker run --rm -u $(UID):$(GID) -v "$(CURDIR)":/src -w /src/test/e2e \
	-e HOME=/tmp -e CI=true -e COREPACK_ENABLE_DOWNLOAD_PROMPT=0 \
	-e COREPACK_HOME=/src/.cache/corepack -e pnpm_config_store_dir=/src/.cache/pnpm-store \
	$(NODE_IMAGE) corepack pnpm
GITLEAKS = docker run --rm -v "$(CURDIR)":/repo $(GITLEAKS_IMAGE)

.PHONY: help build test lint fmt tidy gitleaks gitleaks-staged hooks \
	go-build go-test go-lint web-install web-build web-test web-lint e2e e2e-up e2e-run e2e-down e2e-install e2e-lint \
	deploy logs

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

build: web-build go-build ## Build the SPA and the hostbud binary (SPA embedded) into ./bin

test: go-test web-test ## Run Go and frontend unit tests

lint: go-lint web-lint e2e-lint ## Run golangci-lint, eslint and vue-tsc (app and e2e suite)

go-build: ## Build only the Go binary (embeds whatever is in web/dist)
	$(DOCKER_GO) -e CGO_ENABLED=0 $(GO_IMAGE) go build -o bin/hostbud ./cmd/hostbud

go-test: ## Run Go unit tests
	$(DOCKER_GO) $(GO_IMAGE) go test -race ./...

go-lint: ## Run golangci-lint (linters + formatting check)
	$(DOCKER_GO) $(LINT_IMAGE) golangci-lint run ./...

web-install: ## Install frontend dependencies from the lockfile
	$(DOCKER_PNPM) install --frozen-lockfile

web-build: web-install ## Build the SPA into web/dist
	$(DOCKER_PNPM) run build

web-test: web-install ## Run frontend unit tests (Vitest)
	$(DOCKER_PNPM) run test

web-lint: web-install ## Lint (eslint) and type-check (vue-tsc) the frontend
	$(DOCKER_PNPM) run lint

e2e: ## Simulated-user tests on a fresh throwaway stack, torn down after (ARGS=… for playwright)
	test/e2e/run.sh full $(ARGS)

e2e-up: ## Start/update a persistent hostbud-e2e stack (fast loop with e2e-run)
	test/e2e/run.sh up

e2e-run: ## Run Playwright against the persistent stack, leaving it up (ARGS=…)
	test/e2e/run.sh run $(ARGS)

e2e-down: ## Remove the persistent hostbud-e2e stack and its volumes
	test/e2e/run.sh down

e2e-install: ## Install the e2e suite's dependencies from the lockfile
	$(DOCKER_PNPM_E2E) install --frozen-lockfile

e2e-lint: e2e-install ## Lint (eslint) and type-check (tsc) the e2e suite
	$(DOCKER_PNPM_E2E) run lint

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

deploy: ## Build the image and (re)start hostbud + Caddy (docker compose up -d --build)
	docker compose up -d --build

logs: ## Follow the hostbud and Caddy logs
	docker compose logs -f --tail=100
