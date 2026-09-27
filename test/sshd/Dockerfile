# Throwaway SSH target for integration tests (make test) and e2e (make e2e).
# Never a real machine: host keys and the client key are generated per run
# (see keygen.sh) and never committed.
#   WITH_TMUX=0 builds the tmux-less variant ("tmux missing" scenarios).
FROM debian:stable-slim
LABEL hostbud.image="1"

ARG WITH_TMUX=1
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      openssh-server openssh-client openssh-sftp-server tini procps vim htop less locales ca-certificates \
      python3 curl \
 && if [ "$WITH_TMUX" = "1" ]; then apt-get install -y --no-install-recommends tmux; fi \
 && rm -rf /var/lib/apt/lists/* \
 && sed -i 's/^# *en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen \
 && rm -f /etc/ssh/ssh_host_*

# The "host user". '*' (not the default '!') so sshd doesn't treat the
# account as locked; there is no password login either way.
RUN useradd -m -d /home/dev -s /bin/bash dev && usermod -p '*' dev

COPY sshd_config /etc/ssh/sshd_config.d/hostbud-test.conf
COPY entrypoint.sh keygen.sh sshd-ctl.sh /usr/local/bin/
COPY tmux-wrap.sh /usr/local/lib/hostbud/tmux-wrap.sh
COPY sftp-wrap.sh /usr/local/bin/sftp-wrap
# V2-M1 T7: stub claude and codex clients on dev's PATH (never real agents).
COPY stubs/stub.py /usr/local/lib/hostbud/stubs/stub.py
COPY stubs/claude stubs/codex /usr/local/bin/
RUN chmod 755 /usr/local/bin/sftp-wrap /usr/local/bin/claude /usr/local/bin/codex \
 && sed -i '/^Subsystem[[:space:]]sftp[[:space:]]/c\Subsystem sftp /usr/local/bin/sftp-wrap' /etc/ssh/sshd_config \
 && if [ "$WITH_TMUX" = "1" ]; then chmod 755 /usr/local/lib/hostbud/tmux-wrap.sh && ln -s /usr/local/lib/hostbud/tmux-wrap.sh /usr/local/bin/tmux; fi

ENV LANG=en_US.UTF-8
EXPOSE 22
HEALTHCHECK --interval=1s --timeout=2s --retries=30 CMD bash -c '</dev/tcp/127.0.0.1/22'
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
