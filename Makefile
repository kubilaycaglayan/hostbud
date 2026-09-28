# hostbud — every tool runs in a container; the host only needs Docker.
# Caches live in ./.cache (gitignored) so containers can run as your user.

GO_IMAGE       ?= golang:1.27.1-bookworm
LINT_IMAGE     ?= golangci/golangci-lint:v2.14.0
GITLEAKS_IMAGE ?= zricethezav/gitleaks:v8.30.1
NODE_IMAGE     ?= node:24.21.0-bookworm-slim
PYTHON_IMAGE   ?= python:3.13-slim-bookworm

# Tools run in long-lived toolbox containers (scripts/tool.sh): created on
# first use, then reused via `docker exec`. `make tools-down` removes them.
# The Go toolbox adds a passwd entry for your uid (ssh needs one) and joins the
# hostbud-test network, where the integration targets run.
GO       = TOOL_NETWORK=hostbud-test scripts/tool.sh go $$(scripts/go-toolbox-image.sh $(GO_IMAGE)) .
GOLANGCI = scripts/tool.sh lint $(LINT_IMAGE) .
# pnpm comes from corepack (version pinned by each package.json "packageManager").
PNPM     = scripts/tool.sh node $(NODE_IMAGE) web corepack pnpm
PNPM_E2E = scripts/tool.sh node $(NODE_IMAGE) test/e2e corepack pnpm
SHELL_TOOL = scripts/tool.sh shell $(PYTHON_IMAGE) .
GITLEAKS = scripts/tool.sh gitleaks $(GITLEAKS_IMAGE) . gitleaks

.PHONY: help build test lint fmt tidy icons gitleaks gitleaks-staged hooks \
	go-build go-test go-unit test-env test-down go-lint web-install web-build web-test web-lint e2e e2e-up e2e-run e2e-down e2e-install e2e-lint \
	deploy logs backup restore restore-check tools-down docker-clean docs-lint shell-test doctor

help: ## List targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

build: web-build go-build ## Build the SPA and the hostbud binary (SPA embedded) into ./bin

test: go-test web-test shell-test ## Go unit + integration tests (against test/sshd), Vitest and shell checks

lint: go-lint web-lint e2e-lint docs-lint ## Run golangci-lint, eslint, vue-tsc and docs consistency checks

go-build: ## Build only the Go binary (embeds whatever is in web/dist)
	$(GO) env CGO_ENABLED=0 go build -o bin/hostbud ./cmd/hostbud

go-test: test-env ## Go unit + integration tests (-tags=integration, against test/sshd; packages serially, they share its tmux server)
	$(GO) go test -race -tags=integration -p 1 ./...

go-unit: ## Go unit tests only (no containers besides the toolbox)
	$(GO) go test -race ./...

test-env: ## Start integration targets, verify container hardening, and render production/e2e deploy configs
	scripts/test-backup-tools.sh
	scripts/test-sshd.sh up
	scripts/test-postgres-capabilities.sh
	scripts/test-readonly-image.sh
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

icons: web-install ## Generate the committed hostbud PWA icons
	$(PNPM) run icons

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

docs-lint: ## Check README targets, env docs and documentation links
	scripts/check-docs.sh

shell-test: ## Run shell script fixture tests in a container
	$(SHELL_TOOL) python3 scripts/test-agent-status-hook.py
	$(SHELL_TOOL) sh scripts/test-doctor.sh
	$(SHELL_TOOL) sh scripts/test-docs.sh

doctor: ## Read-only checks for a fresh hostbud installation
	sh scripts/doctor.sh

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

backup: ## Write a private PostgreSQL custom-format dump to ./backups/
	@set -eu; umask 077; mkdir -p backups; chmod 700 backups; \
	name=hostbud-$$(date -u +%Y%m%dT%H%M%S%NZ).dump; tmp=/tmp/$$name; \
	: "the app's /tmp is a tmpfs, which docker cp can't read: stream the dump out"; \
	test ! -e "backups/$$name" || { echo "backup already exists: $$name" >&2; exit 1; }; \
	cleanup() { docker compose exec -T hostbud rm -f "$$tmp" >/dev/null 2>&1 || true; }; \
	trap 'cleanup; [ -s "backups/$$name" ] || rm -f "backups/$$name"' EXIT HUP INT TERM; \
	docker compose exec -T hostbud sh -c 'hostbud backup "$$1" >&2 && cat "$$1"' sh "$$tmp" >"backups/$$name"; \
	chmod 600 "backups/$$name"; \
	size=$$(wc -c <"backups/$$name" | tr -d ' '); \
	echo "Backup written: $$name ($$size bytes)"

restore: ## Replace the configured database from FILE after typed confirmation (CONFIRM for non-interactive use)
	@test -n "$(FILE)" || { echo 'usage: make restore FILE=backups/<name>.dump [CONFIRM=<database>]' >&2; exit 2; }
	@scripts/restore.sh "$(FILE)" "$(CONFIRM)"

restore-check: ## Restore FILE into a temporary database and verify it without touching the configured database
	@test -n "$(FILE)" || { echo 'usage: make restore-check FILE=backups/<name>.dump' >&2; exit 2; }
	@scripts/restore-check.sh "$(FILE)"

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
