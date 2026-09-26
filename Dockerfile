# hostbud runtime image (linux/amd64). Built by `make deploy`.
# Tool versions match the pins in the Makefile — keep them in sync.

# ── 1. SPA ───────────────────────────────────────────────────
FROM --platform=linux/amd64 node:24.21.0-bookworm-slim AS web
ENV CI=true COREPACK_ENABLE_DOWNLOAD_PROMPT=0
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN corepack pnpm install --frozen-lockfile
COPY web/ ./
# "1" only for the e2e image (test/e2e/compose.yml): adds window.__hostbud
# test hooks. Production builds leave it empty.
ARG VITE_E2E=""
RUN VITE_E2E="$VITE_E2E" corepack pnpm run build

# ── 2. Go binary with the SPA embedded ───────────────────────
FROM --platform=linux/amd64 golang:1.27.1-bookworm AS go
WORKDIR /src
COPY go.* ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist/ web/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hostbud ./cmd/hostbud

# ── 3. Runtime ───────────────────────────────────────────────
FROM --platform=linux/amd64 debian:stable-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client postgresql-client ca-certificates tini \
 && rm -rf /var/lib/apt/lists/*

# ssh refuses to run for a uid without a passwd entry, so create the host
# user's uid/gid in the image. /data is chowned so the named volume
# (initialised from the image on first use) is writable by that user.
ARG HOST_UID=1000
ARG HOST_GID=1000
RUN groupadd -o -g "${HOST_GID}" hostbud \
 && useradd -o -u "${HOST_UID}" -g "${HOST_GID}" -m -d /home/hostbud -s /usr/sbin/nologin hostbud \
 && install -d -o "${HOST_UID}" -g "${HOST_GID}" -m 0700 /data

COPY --from=go /out/hostbud /usr/local/bin/hostbud

USER ${HOST_UID}:${HOST_GID}
ENV HOSTBUD_LISTEN=:8080 HOSTBUD_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/hostbud"]
