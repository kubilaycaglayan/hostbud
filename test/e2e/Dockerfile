# hostbud-e2e-runner: Playwright (browsers preinstalled) + the suite's deps.
# The specs are bind-mounted by compose.yml, so editing them needs no rebuild.
# Keep the image tag in sync with @playwright/test in package.json.
FROM mcr.microsoft.com/playwright:v1.63.0-noble
ENV CI=true COREPACK_ENABLE_DOWNLOAD_PROMPT=0
WORKDIR /e2e
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN corepack pnpm install --frozen-lockfile
# Stays up (`sleep infinity`); run.sh execs `playwright test` into it, which
# is much faster than a fresh container per run.
ENTRYPOINT ["sleep", "infinity"]
