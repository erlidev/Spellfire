#!/usr/bin/env bash
# Default-deny egress with a narrow allowlist.
# Runs as root inside the container's own network namespace.
set -euo pipefail

ALLOWED_DOMAINS=(
  # Claude
  api.anthropic.com
  console.anthropic.com
  statsig.anthropic.com
  sentry.io
  # Source control
  github.com
  api.github.com
  codeload.github.com
  objects.githubusercontent.com
  raw.githubusercontent.com
  # Package registries — trim to only what your project needs
  registry.npmjs.org
  pypi.org
  files.pythonhosted.org
  crates.io
  static.crates.io
  index.crates.io
  proxy.golang.org
  sum.golang.org
  # Miscellaneous domains
  llama.erli.xyz
)

echo "[firewall] flushing"
iptables -F; iptables -X
iptables -t nat -F; iptables -t nat -X
iptables -t mangle -F; iptables -t mangle -X
ipset destroy allowed-domains 2>/dev/null || true

# Loopback and established flows.
iptables -A INPUT  -i lo -j ACCEPT
iptables -A OUTPUT -o lo -j ACCEPT
iptables -A INPUT  -m state --state ESTABLISHED,RELATED -j ACCEPT
iptables -A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT

# DNS must be open or nothing resolves. This is the one channel that
# remains available for low-bandwidth exfiltration; accept that tradeoff
# or point --dns at a logging resolver you control.
iptables -A OUTPUT -p udp --dport 53 -j ACCEPT
iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT

# Let the VS Code server talk to the host over the container gateway.
HOST_NET="$(ip route | awk '/default/ {print $3}')"
iptables -A INPUT  -s "${HOST_NET}" -j ACCEPT
iptables -A OUTPUT -d "${HOST_NET}" -j ACCEPT

echo "[firewall] resolving allowlist"
ipset create allowed-domains hash:net
for domain in "${ALLOWED_DOMAINS[@]}"; do
  ips="$(dig +short A "$domain" | grep -E '^[0-9]+\.' || true)"
  if [[ -z "$ips" ]]; then
    echo "[firewall] WARN: no A record for $domain" >&2
    continue
  fi
  while read -r ip; do
    ipset add allowed-domains "$ip" 2>/dev/null || true
  done <<< "$ips"
done

iptables -A OUTPUT -m set --match-set allowed-domains dst -j ACCEPT

# Default deny.
iptables -P INPUT   DROP
iptables -P FORWARD DROP
iptables -P OUTPUT  DROP

echo "[firewall] verifying"
if curl -fsS --max-time 5 https://api.anthropic.com/ -o /dev/null 2>&1 \
   || curl -sS --max-time 5 -o /dev/null -w '%{http_code}' https://api.anthropic.com/ | grep -qE '^[0-9]'; then
  echo "[firewall] allowlist reachable: OK"
else
  echo "[firewall] ERROR: api.anthropic.com unreachable" >&2; exit 1
fi

if curl -sS --max-time 5 https://example.com -o /dev/null 2>&1; then
  echo "[firewall] ERROR: default-deny is not in effect" >&2; exit 1
else
  echo "[firewall] default-deny confirmed: OK"
fi
