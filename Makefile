# hostbud — every tool runs in a container; the host only needs Docker.
# Caches live in ./.cache (gitignored) so containers can run as your user.

GO_IMAGE       ?= golang:1.27.1-bookworm
LINT_IMAGE     ?= golangci/golangci-lint:v2.14.0
GITLEAKS_IMAGE ?= zricethezav/gitleaks:v8.30.1
NODE_IMAGE     ?= node:24.21.0-bookworm-slim

# Tools run in long-lived toolbox containers (scripts/tool.sh): created on
# first use, then reused via `docker exec`. `make tools-down` removes them.
# The Go toolbox adds a passwd entry for your uid (ssh needs one) and joins the
# hostbud-test network, where the integration targets run.
GO       = TOOL_NETWORK=hostbud-test scripts/tool.sh go $$(scripts/go-toolbox-image.sh $(GO_IMAGE)) .
GOLANGCI = scripts/tool.sh lint $(LINT_IMAGE) .
# pnpm comes from corepack (version pinned by each package.json "packageManager").
PNPM     = scripts/tool.sh node $(NODE_IMAGE) web corepack pnpm
PNPM_E2E = scripts/tool.sh node $(NODE_IMAGE) test/e2e corepack pnpm
GITLEAKS = scripts/tool.sh gitleaks $(GITLEAKS_IMAGE) . gitleaks

.PHONY: help build test lint fmt tidy gitleaks gitleaks-staged hooks \
	go-build go-test go-unit test-env test-down go-lint web-install web-build web-test web-lint e2e e2e-up e2e-run e2e-down e2e-install e2e-lint \
	deploy logs backup tools-down docker-clean

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

build: web-build go-build ## Build the SPA and the hostbud binary (SPA embedded) into ./bin

test: go-test web-test ## Go unit + integration tests (against test/sshd) and Vitest

lint: go-lint web-lint e2e-lint ## Run golangci-lint, eslint and vue-tsc (app and e2e suite)

go-build: ## Build only the Go binary (embeds whatever is in web/dist)
	$(GO) env CGO_ENABLED=0 go build -o bin/hostbud ./cmd/hostbud

go-test: test-env ## Go unit + integration tests (-tags=integration, against test/sshd; packages serially, they share its tmux server)
	$(GO) go test -race -tags=integration -p 1 ./...

go-unit: ## Go unit tests only (no containers besides the toolbox)
	$(GO) go test -race ./...

test-env: ## Start the integration targets (hostbud-test-sshd[-notmux]; kept running) and render the deploy and Caddy config
	scripts/test-sshd.sh up
	scripts/compose-config.sh
	scripts/caddy-config.sh

test-down: ## Remove the integration targets, their network and keys
	scripts/test-sshd.sh down

go-lint: ## Run golangci-lint (linters + formatting check)
	$(GOLANGCI) golangci-lint run ./...

web-install: ## Install frontend dependencies from the lockfile
	$(PNPM) install --frozen-lockfile

web-build: web-install ## Build the SPA into web/dist
	$(PNPM) run build

web-test: web-install ## Run frontend unit tests (Vitest)
	$(PNPM) run test

web-lint: web-install ## Lint (eslint) and type-check (vue-tsc) the frontend
	$(PNPM) run lint

e2e: ## Simulated-user tests on a fresh throwaway stack, torn down after (ARGS=… for playwright)
	test/e2e/run.sh full $(ARGS)

e2e-up: ## Start/update a persistent hostbud-e2e stack (fast loop with e2e-run)
	test/e2e/run.sh up

e2e-run: ## Run Playwright against the persistent stack, leaving it up (ARGS=…)
	test/e2e/run.sh run $(ARGS)

e2e-down: ## Remove the persistent hostbud-e2e stack and its volumes
	test/e2e/run.sh down

e2e-install: ## Install the e2e suite's dependencies from the lockfile
	$(PNPM_E2E) install --frozen-lockfile

e2e-lint: e2e-install ## Lint (eslint) and type-check (tsc) the e2e suite
	$(PNPM_E2E) run lint

fmt: ## Format Go code
	$(GOLANGCI) golangci-lint fmt ./...

tidy: ## go mod tidy
	$(GO) go mod tidy

gitleaks: ## Scan the whole git history for secrets
	$(GITLEAKS) git /src --no-banner --redact

gitleaks-staged: ## Scan staged changes for secrets (used by the pre-commit hook)
	@$(GITLEAKS) git /src --no-banner --redact --staged

hooks: ## Install the repo's git hooks (gitleaks pre-commit)
	git config core.hooksPath .githooks
	@echo "hooks installed from .githooks/"

deploy: ## Build the image and (re)start hostbud + Caddy (docker compose up -d --build), then drop the images it replaced
	docker compose up -d --build
	@docker image prune -f --filter label=hostbud.image=1 >/dev/null

logs: ## Follow the hostbud and Caddy logs
	docker compose logs -f --tail=100

backup: ## Copy the running hostbud's database to ./backups/ (VACUUM INTO)
	@mkdir -p backups
	@name=hostbud-$$(date -u +%Y%m%dT%H%M%SZ).db; \
	docker compose exec -T hostbud hostbud backup /data/$$name && \
	docker compose cp hostbud:/data/$$name backups/$$name && \
	docker compose exec -T hostbud rm -f /data/$$name && \
	echo "backup written to backups/$$name"

tools-down: ## Remove the toolbox containers (recreated on next use)
	-docker rm -f $$(docker ps -aq --filter label=hostbud.tools=1) 2>/dev/null

docker-clean: ## Free hostbud's Docker disk: untagged images, the e2e stack and images, toolbox containers (CACHE=1 also prunes build cache >72h, all projects)
	-test/e2e/run.sh down
	-docker rm -f $$(docker ps -aq --filter label=hostbud.tools=1) 2>/dev/null
	-docker rmi hostbud-e2e-app:local hostbud-e2e-target:local hostbud-e2e-target-notmux:local \
		hostbud-e2e-caddy:local hostbud-e2e-ctl:local hostbud-e2e-runner:local 2>/dev/null
	docker image prune -f --filter label=hostbud.image=1
	@if [ "$(CACHE)" = 1 ]; then docker builder prune -af --filter until=72h; fi
	@docker system df
