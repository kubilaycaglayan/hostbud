# Go toolbox for make (scripts/tool.sh): the pinned golang image plus a passwd
# entry for the invoking uid (ssh refuses to run without one; integration
# tests run the system ssh against test/sshd).
ARG GO_IMAGE
FROM ${GO_IMAGE}
LABEL hostbud.image="1"
# Same caches as scripts/tool.sh passes, so a `docker exec` without them still
# writes to ./.cache instead of growing the container's own layer.
ENV GOMODCACHE=/src/.cache/gomod GOCACHE=/src/.cache/gobuild GOLANGCI_LINT_CACHE=/src/.cache/golangci
ARG UID=1000
ARG GID=1000
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client postgresql-client \
 && rm -rf /var/lib/apt/lists/* \
 && (getent group "${GID}" || groupadd -o -g "${GID}" hostbud) \
 && useradd -o -u "${UID}" -g "${GID}" -M -d /tmp -s /bin/sh hostbud
