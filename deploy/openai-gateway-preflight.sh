#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-}"
API_KEY="${API_KEY:-}"
ACCOUNT_ID="${ACCOUNT_ID:-}"
GATEWAY_TOKEN="${GATEWAY_TOKEN:-}"
MODEL="${MODEL:-gpt-5.4}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-20}"

if [[ -z "${BASE_URL}" ]]; then
  echo "BASE_URL is required"
  echo "example: BASE_URL=https://api.example.com $0"
  exit 1
fi

trimmed_base="${BASE_URL%/}"

curl_json() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  shift 3 || true
  local headers=("$@")

  if [[ -n "${body}" ]]; then
    curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}" \
      "${headers[@]}" \
      -H "Content-Type: application/json" \
      --data "${body}"
  else
    curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}" \
      "${headers[@]}"
  fi
}

pretty_print() {
  if command -v jq >/dev/null 2>&1; then
    jq .
  else
    cat
  fi
}

gateway_headers=()
if [[ -n "${GATEWAY_TOKEN}" ]]; then
  gateway_headers+=(-H "X-OpenAI-Gateway-Token: ${GATEWAY_TOKEN}")
fi

echo "==> [1/4] app health"
curl_json GET "${trimmed_base}/health" "" | pretty_print
echo

echo "==> [2/4] openai gateway health"
curl_json GET "${trimmed_base}/openai/_health" "" "${gateway_headers[@]}" | pretty_print
echo

if [[ -n "${ACCOUNT_ID}" ]]; then
  echo "==> [3/4] openai gateway verify"
  curl_json GET "${trimmed_base}/openai/_verify?account_id=${ACCOUNT_ID}&transport=http" "" "${gateway_headers[@]}" | pretty_print
  echo
else
  echo "==> [3/4] openai gateway verify skipped (ACCOUNT_ID not set)"
  echo
fi

if [[ -n "${API_KEY}" ]]; then
  echo "==> [4/4] openai responses smoke"
  curl_json POST "${trimmed_base}/v1/responses" "{\"model\":\"${MODEL}\",\"input\":\"preflight hello\"}" \
    -H "Authorization: Bearer ${API_KEY}" \
    "${gateway_headers[@]}" | pretty_print
  echo
else
  echo "==> [4/4] openai responses smoke skipped (API_KEY not set)"
  echo
fi

echo "preflight completed"
