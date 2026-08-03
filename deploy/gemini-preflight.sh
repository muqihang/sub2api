#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-}"
ADMIN_HEADER_NAME="${ADMIN_HEADER_NAME:-Authorization}"
ADMIN_HEADER_VALUE="${ADMIN_HEADER_VALUE:-}"
ACCOUNT_ID="${ACCOUNT_ID:-}"
USER_API_KEY="${USER_API_KEY:-}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-20}"

if [[ -z "${BASE_URL}" || -z "${ADMIN_HEADER_VALUE}" || -z "${ACCOUNT_ID}" ]]; then
  echo "BASE_URL, ADMIN_HEADER_VALUE, and ACCOUNT_ID are required" >&2
  exit 1
fi

trimmed_base="${BASE_URL%/}"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

curl_json() {
  local method="$1"
  local url="$2"
  local outfile="$3"
  shift 3
  curl -fsS --max-time "${TIMEOUT_SECONDS}" -X "${method}" \
    -H "${ADMIN_HEADER_NAME}: ${ADMIN_HEADER_VALUE}" \
    "$@" \
    "${url}" > "${outfile}"
}

assert_no_secret_leak() {
  local file="$1"
  python3 - "$file" <<'PY'
import json, sys

forbidden = {"access_token", "refresh_token", "api_key", "service_account_json", "private_key"}

def walk(node):
    if isinstance(node, dict):
        for key, value in node.items():
            if key in forbidden:
                raise SystemExit(f"secret leak detected for key: {key}")
            walk(value)
    elif isinstance(node, list):
        for item in node:
            walk(item)

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    walk(json.load(fh))
PY
}

assert_contract() {
  local file="$1"
  local mode="$2"
  python3 - "$file" "$mode" "$ACCOUNT_ID" <<'PY'
import json
import sys

path = sys.argv[1]
mode = sys.argv[2]
account_id = sys.argv[3]

with open(path, "r", encoding="utf-8") as fh:
    payload = json.load(fh)

if payload.get("code") != 0:
    raise SystemExit(f"expected success envelope, got code={payload.get('code')}")

data = payload.get("data")
if not isinstance(data, dict):
    raise SystemExit("expected response.data object")

if mode == "health":
    if data.get("gateway_status") not in {"healthy", "degraded"}:
        raise SystemExit("invalid gateway_status")
    if data.get("oauth_status") not in {"healthy", "degraded"}:
        raise SystemExit("invalid oauth_status")
    if data.get("gateway_status") != "healthy":
        raise SystemExit("gateway_status is not healthy")
    if data.get("oauth_status") != "healthy":
        raise SystemExit("oauth_status is not healthy")
    if not isinstance(data.get("gemini_accounts_total"), int):
        raise SystemExit("gemini_accounts_total missing")
    if not isinstance(data.get("accounts_by_family"), dict):
        raise SystemExit("accounts_by_family missing")
    policy = data.get("policy")
    if not isinstance(policy, dict):
        raise SystemExit("policy missing")
    if policy.get("token_cache_mode") not in {"plaintext", "disabled", "encrypted"}:
        raise SystemExit("invalid token_cache_mode")
    if policy.get("session_store") not in {"memory", "redis", "custom", "unknown"}:
        raise SystemExit("invalid session_store")
    if policy.get("production_mode") and policy.get("token_cache_mode") == "plaintext":
        raise SystemExit("production token_cache_mode must not be plaintext")
    if policy.get("production_mode") and policy.get("session_store") == "memory":
        raise SystemExit("production session_store must not be memory")
    if policy.get("production_mode") and not policy.get("thought_signature_session_safety"):
        raise SystemExit("production thought_signature_session_safety must be enabled")
    if not isinstance(data.get("warning_codes"), list):
        raise SystemExit("warning_codes missing")
elif mode == "verify":
    if str(data.get("account_id")) != account_id:
        raise SystemExit("account_id mismatch")
    runtime = data.get("runtime_contract")
    if not isinstance(runtime, dict):
        raise SystemExit("runtime_contract missing")
    if not runtime.get("account_family"):
        raise SystemExit("runtime_contract.account_family missing")
    if not runtime.get("upstream_family"):
        raise SystemExit("runtime_contract.upstream_family missing")
    if data.get("project_id_status") not in {"present", "required_missing", "optional_empty", "unreadable"}:
        raise SystemExit("invalid project_id_status")
    if data.get("project_id_status") == "unreadable" and not data.get("project_id_reason"):
        raise SystemExit("project_id_reason missing for unreadable project_id_status")
    if data.get("project_id_status") == "required_missing":
        raise SystemExit("required project_id is missing")
    if data.get("project_id_status") == "unreadable":
        raise SystemExit("project_id is unreadable")
    if data.get("tier_status") not in {"present", "missing", "default_fallback"}:
        raise SystemExit("invalid tier_status")
    if data.get("token_cache_state") not in {"ok", "degraded"}:
        raise SystemExit("invalid token_cache_state")
    if data.get("oauth_state") not in {"ok", "degraded"}:
        raise SystemExit("invalid oauth_state")
    if data.get("token_cache_state") == "degraded":
        raise SystemExit("token_cache_state is degraded")
    if data.get("oauth_state") == "degraded":
        raise SystemExit("oauth_state is degraded")
    if data.get("session_store") not in {"memory", "redis", "custom", "unknown"}:
        raise SystemExit("invalid session_store")
    if not isinstance(data.get("sticky_session_safety_required"), bool):
        raise SystemExit("sticky_session_safety_required missing")
    if not data.get("sticky_session_safety_required"):
        raise SystemExit("sticky_session_safety_required must be true")
else:
    raise SystemExit(f"unknown mode: {mode}")
PY
}

echo "[1/3] admin gemini health"
health_json="${tmpdir}/health.json"
curl_json GET "${trimmed_base}/api/v1/admin/gemini/health" "${health_json}"
assert_no_secret_leak "${health_json}"
assert_contract "${health_json}" health
cat "${health_json}"

echo "[2/3] admin gemini verify"
verify_json="${tmpdir}/verify.json"
curl_json GET "${trimmed_base}/api/v1/admin/gemini/verify?account_id=${ACCOUNT_ID}" "${verify_json}"
assert_no_secret_leak "${verify_json}"
assert_contract "${verify_json}" verify
cat "${verify_json}"

if [[ -n "${USER_API_KEY}" ]]; then
  echo "[3/3] public gemini smoke"
  smoke_json="${tmpdir}/models.json"
  curl -fsS --max-time "${TIMEOUT_SECONDS}" \
    -H "Authorization: Bearer ${USER_API_KEY}" \
    "${trimmed_base}/v1beta/models" > "${smoke_json}"
  assert_no_secret_leak "${smoke_json}"
  cat "${smoke_json}"
else
  echo "[3/3] public gemini smoke skipped (USER_API_KEY not set)"
fi

echo "gemini preflight completed"
