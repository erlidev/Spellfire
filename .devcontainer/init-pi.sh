#!/usr/bin/env bash
set -euo pipefail

AGENT_DIR="$HOME/.pi/agent"
mkdir -p "$AGENT_DIR"

cat > "$AGENT_DIR/models.json" <<'JSON'
{
  "providers": {
    "llama-server": {
      "baseUrl": "https://llama.erli.xyz/v1",
      "api": "openai-completions",
      "apiKey": "$LLAMA_SERVER_API_KEY",
      "headers": {
        "User-Agent": "pi-agent/1.0"
      },
      "models": [
        {
          "id": "qwen3.6-27b",
          "name": "Qwen3.6 27B",
          "reasoning": true,
          "compat": {
            "supportsDeveloperRole": true,
            "supportsReasoningEffort": false
          },
          "input": ["text"],
          "contextWindow": 131000,
          "maxTokens": 32000,
          "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 }
        }
      ]
    }
  }
}
JSON

chmod 0600 "$AGENT_DIR/models.json"