#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${ZHUMENG_API_BASE:=http://198.12.67.185:18080}"
: "${ZHUMENG_CAPTURE_LEVEL:=deep}"
if [[ -z "${ZHUMENG_API_KEY:-}" ]]; then
  cat >&2 <<'EOF'
请先在当前终端设置你的逐梦/Sub2API API Key：

  export ZHUMENG_API_KEY='你的后台 API Key'

然后重新运行：

  tools/claude-code-lab-start.sh

注意：不要把本机 Claude 个人 token 填到这里；这里应该填逐梦后台生成的 API Key。
EOF
  exit 2
fi

exec python3 "$ROOT/tools/claude_code_lab_capture.py" --api-base "$ZHUMENG_API_BASE" --capture-level "$ZHUMENG_CAPTURE_LEVEL" -- "$@"
