#!/usr/bin/env bash
set -euo pipefail
MODE='localhost-preflight'
: "${CC_GATEWAY_TOKEN:?CC_GATEWAY_TOKEN is required}"
if [[ "false" == "true" ]]; then
  : "${CC_EGRESS_PROXY_URL:?CC_EGRESS_PROXY_URL is required for real modes}"
  if [[ "${ALLOW_REAL_ANTHROPIC_CANARY:-}" != "1" ]]; then
    echo "ALLOW_REAL_ANTHROPIC_CANARY=1 is required for $MODE" >&2
    exit 7
  fi
else
  export ALLOW_REAL_ANTHROPIC_CANARY=0
fi

export CC_GATEWAY_RAW_CAPTURE_DIR='/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/runtime-productization/2026-05-24-cli-through/localhost-preflight/raw'
mkdir -p "$CC_GATEWAY_RAW_CAPTURE_DIR"
chmod 700 "$CC_GATEWAY_RAW_CAPTURE_DIR"
echo "runtime mode: $MODE"
echo "config: cc-gateway.yaml"
