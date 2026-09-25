# Go toolbox for make (scripts/tool.sh): the pinned golang image plus a passwd
# entry for the invoking uid (ssh refuses to run without one; integration
# tests run the system ssh against test/sshd).
ARG GO_IMAGE
FROM ${GO_IMAGE}
ARG UID=1000
ARG GID=1000
RUN apt-get update \
 && apt-get install -y --no-install-recommends openssh-client \
 && rm -rf /var/lib/apt/lists/* \
 && (getent group "${GID}" || groupadd -o -g "${GID}" hostbud) \
 && useradd -o -u "${UID}" -g "${GID}" -M -d /tmp -s /bin/sh hostbud
