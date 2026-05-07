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
tmpdir="$(mktemp -d)"

cleanup() {
  rm -rf "${tmpdir}"
}

trap cleanup EXIT

curl_json() {
  local method="$1"
  local url="$2"
  local body="${3:-}"
  shift 3 || true
  local headers=("$@")

  if [[ -n "${body}" ]]; then
    if ((${#headers[@]} > 0)); then
      curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}" \
        "${headers[@]}" \
        -H "Content-Type: application/json" \
        --data "${body}"
    else
      curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}" \
        -H "Content-Type: application/json" \
        --data "${body}"
    fi
  else
    if ((${#headers[@]} > 0)); then
      curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}" \
        "${headers[@]}"
    else
      curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" "${url}"
    fi
  fi
}

pretty_print() {
  if command -v jq >/dev/null 2>&1; then
    jq .
  else
    cat
  fi
}

assert_safe_json_output() {
  local label="$1"
  local file="$2"

  if command -v jq >/dev/null 2>&1; then
    local unsafe_values
    unsafe_values="$(
      jq -r '
        .. | objects | to_entries[]? |
        select(.key | ascii_downcase | test("^(access_token|refresh_token|id_token|api_key)$")) |
        .value |
        select(type == "string") |
        select(. != "" and . != "***" and . != "<redacted>")
      ' "${file}"
    )"
    if [[ -n "${unsafe_values}" ]]; then
      echo "unsafe output detected in ${label}: sensitive credential field value exposed" >&2
      return 1
    fi
  else
    local sensitive_matches
    sensitive_matches="$(grep -Eio '"(access_token|refresh_token|id_token|api_key)"[[:space:]]*:[[:space:]]*"[^"]+"' "${file}" || true)"
    if [[ -n "${sensitive_matches}" ]] && printf '%s\n' "${sensitive_matches}" \
      | grep -Eiv ':[[:space:]]*"(\*\*\*|<redacted>)"$' \
      | grep -q '.'; then
      echo "unsafe output detected in ${label}: sensitive credential field value exposed" >&2
      return 1
    fi
  fi

  if grep -E 'sk-[A-Za-z0-9_-]{12,}|://[^/@[:space:]]+@' "${file}" >/dev/null 2>&1; then
    echo "unsafe output detected in ${label}: raw secret or proxy credentials exposed" >&2
    return 1
  fi

  if grep -Eiq 'Bearer [A-Za-z0-9._~+/=-]{16,}' "${file}"; then
    echo "unsafe output detected in ${label}: bearer token exposed" >&2
    return 1
  fi
}

capture_and_check_json() {
  local label="$1"
  local outfile="$2"
  shift 2

  "$@" >"${outfile}"
  pretty_print <"${outfile}"
  assert_safe_json_output "${label}" "${outfile}"
}

gateway_headers=()
if [[ -n "${GATEWAY_TOKEN}" ]]; then
  gateway_headers+=(-H "X-OpenAI-Gateway-Token: ${GATEWAY_TOKEN}")
fi

echo "==> [1/4] app health"
capture_and_check_json "app health" "${tmpdir}/health.json" \
  curl_json GET "${trimmed_base}/health" ""
echo

echo "==> [2/4] openai gateway health"
if ((${#gateway_headers[@]} > 0)); then
  capture_and_check_json "openai gateway health" "${tmpdir}/openai-health.json" \
    curl_json GET "${trimmed_base}/openai/_health" "" "${gateway_headers[@]}"
else
  capture_and_check_json "openai gateway health" "${tmpdir}/openai-health.json" \
    curl_json GET "${trimmed_base}/openai/_health" ""
fi
echo

if [[ -n "${ACCOUNT_ID}" ]]; then
  echo "==> [3/4] openai gateway verify"
  if ((${#gateway_headers[@]} > 0)); then
    capture_and_check_json "openai gateway verify" "${tmpdir}/openai-verify.json" \
      curl_json GET "${trimmed_base}/openai/_verify?account_id=${ACCOUNT_ID}&transport=http" "" "${gateway_headers[@]}"
  else
    capture_and_check_json "openai gateway verify" "${tmpdir}/openai-verify.json" \
      curl_json GET "${trimmed_base}/openai/_verify?account_id=${ACCOUNT_ID}&transport=http" ""
  fi
  echo
else
  echo "==> [3/4] openai gateway verify skipped (ACCOUNT_ID not set)"
  echo
fi

if [[ -n "${API_KEY}" ]]; then
  echo "==> [4/4] openai responses smoke"
  if ((${#gateway_headers[@]} > 0)); then
    capture_and_check_json "openai responses smoke" "${tmpdir}/openai-responses.json" \
      curl_json POST "${trimmed_base}/v1/responses" "{\"model\":\"${MODEL}\",\"input\":\"preflight hello\"}" \
        -H "Authorization: Bearer ${API_KEY}" \
        "${gateway_headers[@]}"
  else
    capture_and_check_json "openai responses smoke" "${tmpdir}/openai-responses.json" \
      curl_json POST "${trimmed_base}/v1/responses" "{\"model\":\"${MODEL}\",\"input\":\"preflight hello\"}" \
        -H "Authorization: Bearer ${API_KEY}"
  fi
  echo
else
  echo "==> [4/4] openai responses smoke skipped (API_KEY not set)"
  echo
fi

echo "preflight completed"
