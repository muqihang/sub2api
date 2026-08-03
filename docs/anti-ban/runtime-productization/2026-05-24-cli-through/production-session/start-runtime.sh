#!/usr/bin/env bash
set -euo pipefail
MODE='production-session'
: "${CC_GATEWAY_TOKEN:?CC_GATEWAY_TOKEN is required}"
if [[ "true" == "true" ]]; then
  : "${CC_EGRESS_PROXY_URL:?CC_EGRESS_PROXY_URL is required for real modes}"
fi

if [[ "${ALLOW_REAL_ANTHROPIC_PRODUCTION:-}" != "1" ]]; then
  echo "ALLOW_REAL_ANTHROPIC_PRODUCTION=1 is required for $MODE" >&2
  exit 7
fi

echo "runtime mode: $MODE"
echo "config: cc-gateway.yaml"
