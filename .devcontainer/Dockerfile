ARG NODE_VERSION=22
FROM node:${NODE_VERSION}-bookworm

ARG TZ
ENV TZ="$TZ"
ENV DEVCONTAINER=true

# --- base tooling -----------------------------------------------------------
# iptables/ipset/dnsutils: egress allowlist
# bubblewrap/socat:        Claude Code's built-in bash sandbox (defence in depth)
# jq/curl/git/gh:          agent workflow essentials
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget git gnupg less procps \
      iptables ipset iproute2 dnsutils \
      bubblewrap socat \
      jq ripgrep fd-find fish sudo man-db unzip \
  && rm -rf /var/lib/apt/lists/*

# GitHub CLI (optional, but agents reach for it constantly)
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
      -o /usr/share/keyrings/githubcli.gpg \
  && echo "deb [signed-by=/usr/share/keyrings/githubcli.gpg] https://cli.github.com/packages stable main" \
      > /etc/apt/sources.list.d/github-cli.list \
  && apt-get update && apt-get install -y --no-install-recommends gh \
  && rm -rf /var/lib/apt/lists/*

# --- persisted dirs ---------------------------------------------------------
RUN mkdir -p \
      /commandhistory \
      /home/node/.claude \
      /home/node/.vscode-server \
      /home/node/.vscode-server-insiders \
      /vscode \
      /workspace \
  && chown -R node:node \
      /commandhistory \
      /home/node \
      /vscode \
      /workspace

# --- egress firewall --------------------------------------------------------
COPY init-firewall.sh /usr/local/bin/init-firewall.sh
RUN chmod 0755 /usr/local/bin/init-firewall.sh \
  && echo 'node ALL=(root) NOPASSWD: /usr/local/bin/init-firewall.sh' \
       > /etc/sudoers.d/init-firewall \
  && chmod 0440 /etc/sudoers.d/init-firewall

# --- pi agent config --------------------------------------------------------
COPY init-pi.sh /usr/local/bin/init-pi.sh
RUN chmod 0755 /usr/local/bin/init-pi.sh

# --- Claude Code ------------------------------------------------------------
RUN npm install -g @anthropic-ai/claude-code

# --- Pi.dev ---
RUN npm install -g --ignore-scripts @earendil-works/pi-coding-agent

USER node
WORKDIR /workspace

# Bypass mode lives in the CONTAINER's user-level settings, never in the
# repo's .claude/settings.json. A repo-controlled settings file is an
# attack surface (see CVE-2026-33068).
RUN mkdir -p /home/node/.claude \
  && printf '{\n  "permissions": {\n    "defaultMode": "bypassPermissions"\n  }\n}\n' \
       > /home/node/.claude/settings.json

SHELL ["/bin/bash", "-c"]