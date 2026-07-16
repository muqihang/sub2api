#!/usr/bin/env bash

log() {
  printf '[hot-deploy] %s\n' "$*"
  if [[ -n "${DEPLOY_LOG:-}" ]]; then
    printf '[hot-deploy] %s\n' "$*" >>"${DEPLOY_LOG}"
  fi
}

warn() {
  printf '[hot-deploy] WARNING: %s\n' "$*" >&2
  if [[ -n "${DEPLOY_LOG:-}" ]]; then
    printf '[hot-deploy] WARNING: %s\n' "$*" >>"${DEPLOY_LOG}"
  fi
}

die() {
  local status="$1"
  shift
  printf '[hot-deploy] ERROR: %s\n' "$*" >&2
  exit "${status}"
}

usage() {
  cat <<'EOF'
Usage: hot-deploy.sh --image IMAGE [options]

Options:
  --candidate NAME         Candidate container name
  --active-container NAME  Override active application container discovery
  --config FILE            Load deployment settings from FILE
  --skip-api-smoke         Explicitly skip Responses, Compact, and Search smoke probes
  --dry-run                Validate input and print the intended deployment
  --help                   Show this help
EOF
}

apply_defaults() {
  CADDY_CONTAINER="${CADDY_CONTAINER:-sub2api-caddy}"
  CADDY_ADMIN_URL="${CADDY_ADMIN_URL:-http://127.0.0.1:2019/config/}"
  CADDYFILE="${CADDYFILE:-/opt/sub2api/deploy/Caddyfile.zhumeng}"
  PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-https://api.5566676.xyz}"
  APP_PORT="${APP_PORT:-8080}"
  HEALTH_PATH="${HEALTH_PATH:-/health}"
  RESPONSES_PATH="${RESPONSES_PATH:-/responses}"
  COMPACT_PATH="${COMPACT_PATH:-/responses}"
  NATIVE_SEARCH_ROOT_PATH="${NATIVE_SEARCH_ROOT_PATH:-/alpha/search}"
  NATIVE_SEARCH_V1_PATH="${NATIVE_SEARCH_V1_PATH:-/v1/alpha/search}"
  CONTAINER_PREFIX="${CONTAINER_PREFIX:-sub2api-next}"
  SMOKE_MODEL="${SMOKE_MODEL:-gpt-5.6-sol}"
  SMOKE_USER_AGENT="${SMOKE_USER_AGENT:-codex_cli_rs/hot-deploy}"
  SMOKE_ORIGINATOR="${SMOKE_ORIGINATOR:-codex_cli_rs}"
  STATE_DIR="${STATE_DIR:-/opt/sub2api/deploy/hot-deploy-state}"
  LOCK_FILE="${LOCK_FILE:-/var/lock/sub2api-hot-deploy}"
  HEALTH_TIMEOUT_SECONDS="${HEALTH_TIMEOUT_SECONDS:-180}"
  REQUEST_TIMEOUT_SECONDS="${REQUEST_TIMEOUT_SECONDS:-180}"
  SOAK_SECONDS="${SOAK_SECONDS:-30}"
  SOAK_INTERVAL_SECONDS="${SOAK_INTERVAL_SECONDS:-5}"
  COMPACT_SMOKE_BYTES="${COMPACT_SMOKE_BYTES:-1048576}"
  CANDIDATE_STOP_TIMEOUT_SECONDS="${CANDIDATE_STOP_TIMEOUT_SECONDS:-10}"
  IMAGE="${IMAGE:-}"
  CANDIDATE_CONTAINER="${CANDIDATE_CONTAINER:-}"
  ACTIVE_CONTAINER="${ACTIVE_CONTAINER:-}"
  CONFIG_FILE="${CONFIG_FILE:-}"
  DRY_RUN="${DRY_RUN:-false}"
  SKIP_API_SMOKE="${SKIP_API_SMOKE:-false}"
  SMOKE_API_KEY="${SMOKE_API_KEY:-}"
  LOCK_DIR="${LOCK_FILE}.d"
  LOCK_ACQUIRED=false
}

preload_config() {
  local previous=""
  local argument
  for argument in "$@"; do
    if [[ "${previous}" == "--config" ]]; then
      CONFIG_FILE="${argument}"
      break
    fi
    previous="${argument}"
  done

  if [[ -n "${CONFIG_FILE:-}" ]]; then
    [[ -f "${CONFIG_FILE}" ]] || die 2 "config file not found: ${CONFIG_FILE}"
    # Deployment configuration is trusted operator input and uses shell syntax.
    # shellcheck disable=SC1090
    source "${CONFIG_FILE}"
  fi
}

parse_args() {
  while (( $# > 0 )); do
    case "$1" in
      --image)
        (( $# >= 2 )) || die 2 "--image requires a value"
        IMAGE="$2"
        shift 2
        ;;
      --candidate)
        (( $# >= 2 )) || die 2 "--candidate requires a value"
        CANDIDATE_CONTAINER="$2"
        shift 2
        ;;
      --active-container)
        (( $# >= 2 )) || die 2 "--active-container requires a value"
        ACTIVE_CONTAINER="$2"
        shift 2
        ;;
      --config)
        (( $# >= 2 )) || die 2 "--config requires a value"
        CONFIG_FILE="$2"
        shift 2
        ;;
      --skip-api-smoke)
        SKIP_API_SMOKE=true
        shift
        ;;
      --dry-run)
        DRY_RUN=true
        shift
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        die 2 "unknown argument: $1"
        ;;
    esac
  done
}

validate_positive_integer() {
  local name="$1"
  local value="$2"
  case "${value}" in
    ''|*[!0-9]*|0) die 2 "${name} must be a positive integer" ;;
  esac
}

validate_config() {
  [[ -n "${IMAGE}" ]] || die 2 "--image is required"
  if [[ "${SKIP_API_SMOKE}" != "true" && -z "${SMOKE_API_KEY}" ]]; then
    die 2 "SMOKE_API_KEY is required unless --skip-api-smoke is explicit"
  fi

  validate_positive_integer APP_PORT "${APP_PORT}"
  validate_positive_integer HEALTH_TIMEOUT_SECONDS "${HEALTH_TIMEOUT_SECONDS}"
  validate_positive_integer REQUEST_TIMEOUT_SECONDS "${REQUEST_TIMEOUT_SECONDS}"
  validate_positive_integer SOAK_INTERVAL_SECONDS "${SOAK_INTERVAL_SECONDS}"
  validate_positive_integer COMPACT_SMOKE_BYTES "${COMPACT_SMOKE_BYTES}"
  validate_positive_integer CANDIDATE_STOP_TIMEOUT_SECONDS "${CANDIDATE_STOP_TIMEOUT_SECONDS}"
  case "${SOAK_SECONDS}" in
    ''|*[!0-9]*) die 2 "SOAK_SECONDS must be a non-negative integer" ;;
  esac

  if [[ -z "${CANDIDATE_CONTAINER}" ]]; then
    CANDIDATE_CONTAINER="${CONTAINER_PREFIX}-$(printf '%s' "${IMAGE}" | tr '/:@' '---')"
  fi
}

release_lock() {
  if [[ "${LOCK_ACQUIRED}" == "true" ]]; then
    rmdir "${LOCK_DIR}" 2>/dev/null || true
    LOCK_ACQUIRED=false
  fi
}

acquire_lock() {
  if [[ "${FAKE_LOCK_HELD:-0}" == "1" ]]; then
    die 3 "another deployment holds the lock: ${LOCK_FILE}"
  fi
  if [[ "${DRY_RUN}" == "true" ]]; then
    return 0
  fi
  if ! mkdir "${LOCK_DIR}" 2>/dev/null; then
    die 3 "another deployment holds the lock: ${LOCK_FILE}"
  fi
  LOCK_ACQUIRED=true
}

print_dry_run() {
  log "DRY RUN"
  log "Image: ${IMAGE}"
  log "Candidate: ${CANDIDATE_CONTAINER}"
  log "Caddy container: ${CADDY_CONTAINER}"
  log "Caddyfile: ${CADDYFILE}"
  log "Public base URL: ${PUBLIC_BASE_URL}"
  if [[ "${SKIP_API_SMOKE}" == "true" ]]; then
    warn "API smoke: SKIPPED BY OPERATOR"
  else
    log "API smoke: required (key supplied, value redacted)"
  fi
}

snapshot_caddy() {
  local output_file="$1"
  docker exec "${CADDY_CONTAINER}" \
    wget -q -O - "${CADDY_ADMIN_URL}" >"${output_file}"
  [[ -s "${output_file}" ]] || {
    printf 'Caddy Admin API returned an empty configuration\n' >&2
    return 1
  }
}

extract_active_upstream() {
  local config_file="$1"
  python3 - "${config_file}" "${APP_PORT}" <<'PY'
import json
import sys

config_path, app_port = sys.argv[1:]
with open(config_path, "r", encoding="utf-8") as handle:
    config = json.load(handle)

dials = set()

def walk(value):
    if isinstance(value, dict):
        upstreams = value.get("upstreams")
        if isinstance(upstreams, list):
            for upstream in upstreams:
                if isinstance(upstream, dict):
                    dial = upstream.get("dial")
                    if isinstance(dial, str) and dial.rsplit(":", 1)[-1] == app_port:
                        dials.add(dial)
        for child in value.values():
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)

walk(config)
if len(dials) != 1:
    rendered = ", ".join(sorted(dials)) or "none"
    raise SystemExit(
        f"expected exactly one active upstream on port {app_port}, found {len(dials)}: {rendered}"
    )

print(next(iter(dials)))
PY
}

render_candidate_caddyfile() {
  local source_file="$1"
  local output_file="$2"
  local active_upstream="$3"
  local candidate_upstream="$4"
  python3 - "${source_file}" "${output_file}" "${active_upstream}" "${candidate_upstream}" <<'PY'
import pathlib
import sys

source_path, output_path, active, candidate = sys.argv[1:]
if not active or not candidate or any(char in active + candidate for char in "\r\n"):
    raise SystemExit("invalid upstream value")

source = pathlib.Path(source_path).read_text(encoding="utf-8")
count = source.count(active)
if count == 0:
    raise SystemExit(f"host Caddyfile does not contain active upstream: {active}")

rendered = source.replace(active, candidate)
if active in rendered or candidate not in rendered:
    raise SystemExit("candidate Caddyfile replacement verification failed")

pathlib.Path(output_path).write_text(rendered, encoding="utf-8")
PY
}

validate_caddyfile() {
  local candidate_file="$1"
  docker exec -i "${CADDY_CONTAINER}" \
    caddy validate --config - --adapter caddyfile <"${candidate_file}"
}

adapt_caddyfile() {
  local candidate_file="$1"
  local output_file="$2"
  docker exec -i "${CADDY_CONTAINER}" \
    caddy adapt --config - --adapter caddyfile \
    <"${candidate_file}" >"${output_file}"
  [[ -s "${output_file}" ]] || {
    printf 'Caddy adapt returned an empty native configuration\n' >&2
    return 1
  }
}

reload_caddyfile() {
  local candidate_file="$1"
  docker exec -i "${CADDY_CONTAINER}" \
    caddy reload --config - --adapter caddyfile <"${candidate_file}"
}

restore_caddy_json() {
  local saved_config="$1"
  docker exec -i "${CADDY_CONTAINER}" \
    caddy reload --config - <"${saved_config}"
}

assert_active_upstream() {
  local expected_upstream="$1"
  local snapshot_file="$2"
  local actual_upstream
  snapshot_caddy "${snapshot_file}" || return 1
  actual_upstream="$(extract_active_upstream "${snapshot_file}")" || return 1
  if [[ "${actual_upstream}" != "${expected_upstream}" ]]; then
    printf 'active Caddy upstream mismatch: expected %s, got %s\n' \
      "${expected_upstream}" "${actual_upstream}" >&2
    return 1
  fi
}

discover_docker_network() {
  if [[ -n "${DOCKER_NETWORK:-}" ]]; then
    printf '%s\n' "${DOCKER_NETWORK}"
    return 0
  fi

  local network_output
  local network
  local discovered=""
  local count=0
  network_output="$(docker inspect --format \
    '{{range $name, $_ := .NetworkSettings.Networks}}{{println $name}}{{end}}' \
    "${ACTIVE_CONTAINER}")" || return 1
  while IFS= read -r network; do
    [[ -n "${network}" ]] || continue
    discovered="${network}"
    count=$((count + 1))
  done <<<"${network_output}"

  if [[ "${count}" -ne 1 ]]; then
    printf 'expected active container %s on exactly one network, found %s\n' \
      "${ACTIVE_CONTAINER}" "${count}" >&2
    return 1
  fi
  DOCKER_NETWORK="${discovered}"
  export DOCKER_NETWORK
  printf '%s\n' "${DOCKER_NETWORK}"
}

create_candidate() {
  local network
  local env_output
  local env_entry
  local restart_policy
  local ulimit_output
  local ulimit_line
  local ulimit_name
  local ulimit_soft
  local ulimit_hard
  local env_name
  local docker_bin
  local -a create_args
  local -a env_entries

  docker image inspect "${IMAGE}" >/dev/null || {
    printf 'image does not exist: %s\n' "${IMAGE}" >&2
    return 1
  }
  if docker container inspect "${CANDIDATE_CONTAINER}" >/dev/null 2>&1; then
    printf 'candidate container already exists: %s\n' "${CANDIDATE_CONTAINER}" >&2
    return 1
  fi

  network="$(discover_docker_network)" || return 1
  env_output="$(docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' \
    "${ACTIVE_CONTAINER}")" || return 1
  restart_policy="$(docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' \
    "${ACTIVE_CONTAINER}")" || return 1
  TARGET_RESTART_POLICY="${restart_policy:-no}"
  ulimit_output="$(docker inspect --format \
    '{{range .HostConfig.Ulimits}}{{println .Name .Soft .Hard}}{{end}}' \
    "${ACTIVE_CONTAINER}")" || return 1
  docker_bin="$(command -v docker)" || return 1

  create_args=(
    create
    --name "${CANDIDATE_CONTAINER}"
    --network "${network}"
    --volumes-from "${ACTIVE_CONTAINER}"
    --restart no
  )
  while IFS= read -r env_entry; do
    [[ -n "${env_entry}" ]] || continue
    env_name="${env_entry%%=*}"
    if [[ "${env_entry}" == "${env_name}" || ! "${env_name}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
      printf 'active container has an unsupported environment name: %s\n' "${env_name}" >&2
      return 1
    fi
    env_entries+=("${env_entry}")
    create_args+=(--env "${env_name}")
  done <<<"${env_output}"
  while IFS= read -r ulimit_line; do
    [[ -n "${ulimit_line}" ]] || continue
    read -r ulimit_name ulimit_soft ulimit_hard <<<"${ulimit_line}"
    [[ -n "${ulimit_name}" && -n "${ulimit_soft}" && -n "${ulimit_hard}" ]] || {
      printf 'invalid ulimit inspection result: %s\n' "${ulimit_line}" >&2
      return 1
    }
    create_args+=(--ulimit "${ulimit_name}=${ulimit_soft}:${ulimit_hard}")
  done <<<"${ulimit_output}"
  create_args+=("${IMAGE}")

  (
    for env_entry in "${env_entries[@]}"; do
      export "${env_entry}"
    done
    "${docker_bin}" "${create_args[@]}" >/dev/null
  ) || return 1
  CANDIDATE_CREATED=true
  docker start "${CANDIDATE_CONTAINER}" >/dev/null
}

promote_candidate_restart_policy() {
  docker update --restart "${TARGET_RESTART_POLICY:-no}" \
    "${CANDIDATE_CONTAINER}" >/dev/null
}

quarantine_candidate() {
  if [[ "${CANDIDATE_CREATED:-false}" != "true" || \
        "${CANDIDATE_COMMITTED:-false}" == "true" ]]; then
    return 0
  fi
  log "Quarantining failed candidate ${CANDIDATE_CONTAINER} (container retained)"
  docker stop --time "${CANDIDATE_STOP_TIMEOUT_SECONDS}" \
    "${CANDIDATE_CONTAINER}" >/dev/null
}

wait_candidate_ready() {
  local timeout="${1:-${HEALTH_TIMEOUT_SECONDS}}"
  local poll_interval="${HEALTH_POLL_INTERVAL_SECONDS:-2}"
  local started_at="${SECONDS}"
  local state
  local status
  local health

  while true; do
    state="$(docker inspect --format \
      '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' \
      "${CANDIDATE_CONTAINER}")" || return 1
    read -r status health <<<"${state}"
    if [[ "${status}" != "running" ]]; then
      printf 'candidate is not running: status=%s health=%s\n' "${status}" "${health}" >&2
      return 1
    fi
    case "${health}" in
      healthy) return 0 ;;
      unhealthy)
        printf 'candidate reported unhealthy\n' >&2
        return 1
        ;;
      none)
        printf 'candidate image has no healthcheck\n' >&2
        return 1
        ;;
    esac
    if (( SECONDS - started_at >= timeout )); then
      printf 'candidate health check timed out after %s seconds\n' "${timeout}" >&2
      return 1
    fi
    sleep "${poll_interval}"
  done
}

candidate_base_url() {
  local network
  local address
  network="$(discover_docker_network)" || return 1
  address="$(docker inspect --format \
    "{{with index .NetworkSettings.Networks \"${network}\"}}{{.IPAddress}}{{end}}" \
    "${CANDIDATE_CONTAINER}")" || return 1
  [[ -n "${address}" ]] || {
    printf 'candidate has no address on Docker network %s\n' "${network}" >&2
    return 1
  }
  printf 'http://%s:%s\n' "${address}" "${APP_PORT}"
}

prepare_smoke_payloads() {
  RESPONSES_SMOKE_PAYLOAD="${STATE_DIR}/responses-smoke.json"
  COMPACT_SMOKE_PAYLOAD="${STATE_DIR}/compact-smoke.json"
  export RESPONSES_SMOKE_PAYLOAD COMPACT_SMOKE_PAYLOAD
  python3 - \
    "${RESPONSES_SMOKE_PAYLOAD}" \
    "${COMPACT_SMOKE_PAYLOAD}" \
    "${SMOKE_MODEL}" \
    "${COMPACT_SMOKE_BYTES}" <<'PY'
import json
import pathlib
import sys

responses_path, compact_path, model, target_size = sys.argv[1:]
target_size = int(target_size)

responses = {
    "model": model,
    "input": "Reply with exactly OK.",
    "max_output_tokens": 16,
}
pathlib.Path(responses_path).write_text(
    json.dumps(responses, separators=(",", ":")), encoding="utf-8"
)

prefix = "Deployment compact canary. Preserve the marker HOT_DEPLOY_OK. "
repeat = "0123456789abcdef "
text = prefix + (repeat * ((target_size // len(repeat)) + 1))
text = text[:target_size]
compact = {
    "model": model,
    "stream": True,
    "store": True,
    "input": [
        {
            "type": "message",
            "role": "user",
            "content": [{"type": "input_text", "text": text}],
        },
        {"type": "compaction_trigger"},
    ],
}
pathlib.Path(compact_path).write_text(
    json.dumps(compact, separators=(",", ":")), encoding="utf-8"
)
PY
}

assert_health_json() {
  local response_file="$1"
  python3 - "${response_file}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if not isinstance(payload, dict):
    raise SystemExit("health probe response is not a JSON object")
if payload.get("error"):
    raise SystemExit(f"health probe returned an error object: {payload['error']}")
PY
}

assert_responses_json() {
  local response_file="$1"
  python3 - "${response_file}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
if not isinstance(payload, dict):
    raise SystemExit("probe response is not a JSON object")
if payload.get("error"):
    raise SystemExit(f"probe returned an error object: {payload['error']}")
response_id = payload.get("id")
if not isinstance(response_id, str) or not response_id.startswith("resp_"):
    raise SystemExit("Responses smoke returned no response id")
if not isinstance(payload.get("output"), list):
    raise SystemExit("Responses smoke returned no output array")
if payload.get("status") != "completed":
    raise SystemExit(f"Responses smoke returned non-completed status: {payload.get('status')}")
output = payload.get("output")
if not output:
    raise SystemExit("Responses smoke returned empty output")

def collect_text(value):
    found = []
    if isinstance(value, dict):
        text = value.get("text")
        if isinstance(text, str):
            found.append(text)
        for child in value.values():
            found.extend(collect_text(child))
    elif isinstance(value, list):
        for child in value:
            found.extend(collect_text(child))
    return found

texts = collect_text(output)
if not any(text.strip() == "OK" for text in texts):
    raise SystemExit("Responses smoke did not return the OK canary")
PY
}

assert_compact_sse() {
  local response_file="$1"
  python3 - "${response_file}" <<'PY'
import json
import sys

payloads = []
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    for raw_line in handle:
        line = raw_line.strip()
        if not line.startswith("data:"):
            continue
        data = line[5:].strip()
        if not data or data == "[DONE]":
            continue
        payloads.append(json.loads(data))

def contains_compaction(value):
    if isinstance(value, dict):
        if value.get("type") in {"compaction", "compaction_summary"}:
            return True
        return any(contains_compaction(child) for child in value.values())
    if isinstance(value, list):
        return any(contains_compaction(child) for child in value)
    return False

if not payloads:
    raise SystemExit("Compact smoke did not return SSE data events")
if any(item.get("type") in {"error", "response.failed", "response.incomplete"} for item in payloads):
    raise SystemExit("Compact smoke returned a terminal error event")
if not any(contains_compaction(item) for item in payloads):
    raise SystemExit("Compact smoke returned no compaction output item")
if not any(item.get("type") == "response.completed" for item in payloads):
    raise SystemExit("Compact smoke returned no response.completed event")
completed = [item for item in payloads if item.get("type") == "response.completed"][-1]
response = completed.get("response")
if not isinstance(response, dict) or response.get("status") != "completed":
    raise SystemExit("Compact smoke terminal response status is not completed")
PY
}

run_probe() {
  local kind="$1"
  local scope="$2"
  local url="$3"
  local payload_file="${4:-}"
  local output_file="${STATE_DIR}/${scope}-${kind}-response.json"
  local escaped_key

  if [[ "${kind}" == "health" ]]; then
    HOT_DEPLOY_PROBE_SCOPE="${scope}" HOT_DEPLOY_PROBE_KIND="${kind}" \
      curl -fsS --max-time "${REQUEST_TIMEOUT_SECONDS}" --config - \
      >"${output_file}" <<EOF || return 1
url = "${url}"
EOF
  else
    case "${SMOKE_API_KEY}" in
      *$'\n'*|*$'\r'*)
        printf 'SMOKE_API_KEY must not contain a newline\n' >&2
        return 1
        ;;
    esac
    escaped_key="${SMOKE_API_KEY//\\/\\\\}"
    escaped_key="${escaped_key//\"/\\\"}"
    case "${SMOKE_USER_AGENT}${SMOKE_ORIGINATOR}" in
      *$'\n'*|*$'\r'*)
        printf 'Codex smoke identity headers must not contain a newline\n' >&2
        return 1
        ;;
    esac
    HOT_DEPLOY_PROBE_SCOPE="${scope}" HOT_DEPLOY_PROBE_KIND="${kind}" \
      curl -fsS --max-time "${REQUEST_TIMEOUT_SECONDS}" --config - \
      --data-binary "@${payload_file}" >"${output_file}" <<EOF || return 1
url = "${url}"
header = "Authorization: Bearer ${escaped_key}"
header = "Content-Type: application/json"
header = "User-Agent: ${SMOKE_USER_AGENT}"
header = "originator: ${SMOKE_ORIGINATOR}"
EOF
  fi
  case "${kind}" in
    compact) assert_compact_sse "${output_file}" ;;
    responses) assert_responses_json "${output_file}" ;;
    health) assert_health_json "${output_file}" ;;
  esac
}

probe_health() {
  local base_url="${1%/}"
  local scope="$2"
  run_probe health "${scope}" "${base_url}${HEALTH_PATH}"
}

validate_native_search_headers() {
  local verdict_file="$1"
  python3 -c '
import re
import sys

raw = sys.stdin.buffer.read().decode("iso-8859-1", "replace")
blocks = [block for block in re.split(r"\r?\n\r?\n", raw) if block.strip()]
status = None
content_type = ""
for block in blocks:
    lines = block.splitlines()
    if not lines or not lines[0].startswith("HTTP/"):
        continue
    parts = lines[0].split()
    if len(parts) < 2 or not parts[1].isdigit():
        continue
    candidate_status = int(parts[1])
    if 100 <= candidate_status < 200:
        continue
    status = candidate_status
    content_type = ""
    for line in lines[1:]:
        if ":" not in line:
            continue
        key, value = line.split(":", 1)
        if key.strip().lower() == "content-type":
            content_type = value.strip().lower()
if status != 200:
    raise SystemExit("native Search probe returned a non-200 status")
if content_type.split(";", 1)[0].strip() != "application/json":
    raise SystemExit("native Search probe returned a non-JSON Content-Type")
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    handle.write("status=200 content_type=application/json\n")
' "${verdict_file}"
}

validate_native_search_body_stream() {
  python3 -c '
import json
import sys

try:
    payload = json.load(sys.stdin.buffer)
except Exception:
    raise SystemExit("native Search probe returned malformed JSON")
if not isinstance(payload, dict):
    raise SystemExit("native Search probe response is not a JSON object")
output = payload.get("output")
if not isinstance(output, str) or not output.strip():
    raise SystemExit("native Search probe returned no output string")
encrypted = payload.get("encrypted_output")
if encrypted is not None and not isinstance(encrypted, str):
    raise SystemExit("native Search probe returned invalid encrypted_output")
' >/dev/null
}

probe_native_search_path() {
  local base_url="${1%/}"
  local scope="$2"
  local path="$3"
  local kind="$4"
  local escaped_key payload scratch headers_fifo header_verdict header_pid
  local curl_status body_status header_status had_errexit=false
  local -a pipeline_status

  case "${SMOKE_API_KEY}" in
    *$'\n'*|*$'\r'*)
      printf 'SMOKE_API_KEY must not contain a newline\n' >&2
      return 1
      ;;
  esac
  case "${SMOKE_USER_AGENT}${SMOKE_ORIGINATOR}" in
    *$'\n'*|*$'\r'*)
      printf 'Codex smoke identity headers must not contain a newline\n' >&2
      return 1
      ;;
  esac
  escaped_key="${SMOKE_API_KEY//\\/\\\\}"
  escaped_key="${escaped_key//\"/\\\"}"
  payload="$(python3 -c 'import json,sys; print(json.dumps({"id":"hot-deploy-native-search","model":sys.argv[1],"commands":{}}, separators=(",",":")))' "${SMOKE_MODEL}")" || return 1

  scratch="$(mktemp -d "${TMPDIR:-/tmp}/sub2api-native-search-probe.XXXXXX")" || return 1
  headers_fifo="${scratch}/headers.fifo"
  header_verdict="${scratch}/header.verdict"
  mkfifo "${headers_fifo}" || {
    rm -rf "${scratch}"
    return 1
  }
  validate_native_search_headers "${header_verdict}" <"${headers_fifo}" &
  header_pid=$!

  [[ $- == *e* ]] && had_errexit=true
  set +e
  HOT_DEPLOY_PROBE_SCOPE="${scope}" HOT_DEPLOY_PROBE_KIND="${kind}" \
    curl -sS --max-time "${REQUEST_TIMEOUT_SECONDS}" --dump-header "${headers_fifo}" \
    --data-binary @<(printf '%s' "${payload}") --config - <<EOF | validate_native_search_body_stream
url = "${base_url}${path}"
header = "Authorization: Bearer ${escaped_key}"
header = "Content-Type: application/json"
header = "Accept: application/json"
header = "User-Agent: ${SMOKE_USER_AGENT}"
header = "originator: ${SMOKE_ORIGINATOR}"
EOF
  pipeline_status=("${PIPESTATUS[@]}")
  curl_status="${pipeline_status[0]}"
  body_status="${pipeline_status[1]}"
  if [[ "${curl_status}" -ne 0 ]]; then
    kill "${header_pid}" 2>/dev/null || true
    wait "${header_pid}" 2>/dev/null
    header_status=1
  else
    wait "${header_pid}"
    header_status=$?
  fi
  [[ "${had_errexit}" == "true" ]] && set -e

  if [[ "${curl_status}" -ne 0 || "${body_status}" -ne 0 || "${header_status}" -ne 0 || ! -s "${header_verdict}" ]]; then
    rm -rf "${scratch}"
    return 1
  fi
  rm -rf "${scratch}"
  log "Native Search probe passed: scope=${scope} path=${path}"
}

probe_native_search_pair() {
  local base_url="$1"
  local scope="$2"
  probe_native_search_path "${base_url}" "${scope}" "${NATIVE_SEARCH_ROOT_PATH}" native-search-root || return 1
  probe_native_search_path "${base_url}" "${scope}" "${NATIVE_SEARCH_V1_PATH}" native-search-v1
}

probe_api_pair() {
  local base_url="${1%/}"
  local scope="$2"
  prepare_smoke_payloads || return 1
  run_probe responses "${scope}" "${base_url}${RESPONSES_PATH}" \
    "${RESPONSES_SMOKE_PAYLOAD}" || return 1
  run_probe compact "${scope}" "${base_url}${COMPACT_PATH}" \
    "${COMPACT_SMOKE_PAYLOAD}" || return 1
  probe_native_search_pair "${base_url}" "${scope}"
}

require_commands() {
  local command_name
  for command_name in docker curl python3; do
    command -v "${command_name}" >/dev/null 2>&1 || {
      printf 'required command not found: %s\n' "${command_name}" >&2
      return 1
    }
  done
}

initialize_state() {
  local state_root="${STATE_DIR}"
  local run_id
  run_id="$(date -u '+%Y%m%dT%H%M%SZ')-${CANDIDATE_CONTAINER}-${$}"
  STATE_DIR="${state_root%/}/${run_id}"
  mkdir -p "${STATE_DIR}"
  chmod 700 "${STATE_DIR}"
  DEPLOY_LOG="${STATE_DIR}/deploy.log"
  : >"${DEPLOY_LOG}"
  export STATE_DIR DEPLOY_LOG
}

write_file_in_place() {
  local source_file="$1"
  local destination_file="$2"
  python3 - "${source_file}" "${destination_file}" <<'PY'
import os
import pathlib
import sys

source = pathlib.Path(sys.argv[1])
destination = pathlib.Path(sys.argv[2])
before = destination.stat().st_ino
payload = source.read_bytes()
with destination.open("wb") as handle:
    handle.write(payload)
    handle.flush()
    os.fsync(handle.fileno())
after = destination.stat().st_ino
if before != after:
    raise SystemExit("persistent Caddyfile inode changed during in-place write")
if destination.read_bytes() != payload:
    raise SystemExit("persistent Caddyfile verification failed")
PY
}

check_caddy_mount_consistency() {
  local mounted_file="${STATE_DIR}/mounted-Caddyfile"
  if ! docker exec "${CADDY_CONTAINER}" cat /etc/caddy/Caddyfile >"${mounted_file}"; then
    warn "could not read the mounted Caddyfile; reload remains stdin-only"
    return 0
  fi
  if ! cmp -s "${CADDYFILE}" "${mounted_file}"; then
    warn "host and mounted Caddyfile differ (stale bind inode); reload remains stdin-only"
  fi
}

run_soak() {
  local started_at="${SECONDS}"
  if [[ "${SOAK_SECONDS}" -eq 0 ]]; then
    log "Soak: skipped because SOAK_SECONDS=0"
    return 0
  fi
  log "Soak: monitoring public health for ${SOAK_SECONDS}s"
  while (( SECONDS - started_at < SOAK_SECONDS )); do
    sleep "${SOAK_INTERVAL_SECONDS}"
    probe_health "${PUBLIC_BASE_URL}" public || return 1
  done
}

json_config_digest() {
  local config_file="$1"
  python3 - "${config_file}" <<'PY'
import hashlib
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    payload = json.load(handle)
canonical = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
print(hashlib.sha256(canonical).hexdigest())
PY
}

json_configs_equal() {
  local left="$1"
  local right="$2"
  local left_digest
  local right_digest
  [[ -s "${left}" && -s "${right}" ]] || return 1
  left_digest="$(json_config_digest "${left}")" || return 1
  right_digest="$(json_config_digest "${right}")" || return 1
  [[ "${left_digest}" == "${right_digest}" ]]
}

assert_caddy_baseline_unchanged() {
  local snapshot_file="${STATE_DIR}/caddy-before-cutover.json"
  snapshot_caddy "${snapshot_file}" || return 1
  if ! json_configs_equal "${CADDY_BEFORE_JSON}" "${snapshot_file}"; then
    printf 'active Caddy configuration changed before cutover\n' >&2
    return 1
  fi
  if ! cmp -s "${CADDYFILE}" "${CADDYFILE_BACKUP}"; then
    printf 'host Caddyfile changed before cutover\n' >&2
    return 1
  fi
}

assert_final_commit_state() {
  local snapshot_file="${STATE_DIR}/caddy-final-verified.json"
  snapshot_caddy "${snapshot_file}" || return 1
  if ! json_configs_equal "${CADDY_OWNED_JSON}" "${snapshot_file}"; then
    printf 'active Caddy configuration changed before commit\n' >&2
    return 1
  fi
  if ! cmp -s "${CADDY_CANDIDATE_FILE}" "${CADDYFILE}"; then
    printf 'persistent Caddyfile changed before commit\n' >&2
    return 1
  fi
}

rollback_deployment() {
  local failed=0
  local ownership_snapshot="${STATE_DIR}/caddy-rollback-ownership.json"
  local ownership=""
  local host_ownership=""
  [[ "${ROLLBACK_ARMED:-false}" == "true" ]] || return 0
  if cmp -s "${CADDYFILE}" "${CADDYFILE_BACKUP}"; then
    host_ownership=original
  elif [[ -n "${CADDY_CANDIDATE_FILE:-}" ]] && \
       cmp -s "${CADDYFILE}" "${CADDY_CANDIDATE_FILE}"; then
    host_ownership=candidate
  else
    warn "CRITICAL: rollback ownership lost; host Caddyfile is external"
    return 86
  fi
  if ! snapshot_caddy "${ownership_snapshot}"; then
    warn "CRITICAL: rollback ownership lost because active Caddy is unreadable"
    return 86
  fi
  if json_configs_equal "${CADDY_BEFORE_JSON}" "${ownership_snapshot}"; then
    ownership=original
  elif [[ -n "${CADDY_OWNED_JSON:-}" ]] && \
       json_configs_equal "${CADDY_OWNED_JSON}" "${ownership_snapshot}"; then
    ownership=candidate
  else
    warn "CRITICAL: rollback ownership lost; active Caddy digest is external"
    return 86
  fi
  log "ROLLBACK STARTED: restoring ${ORIGINAL_UPSTREAM}"
  if [[ "${ownership}" == "candidate" ]]; then
    restore_caddy_json "${CADDY_BEFORE_JSON}" || failed=1
  fi
  if [[ "${host_ownership}" == "candidate" ]]; then
    write_file_in_place "${CADDYFILE_BACKUP}" "${CADDYFILE}" || failed=1
  fi
  assert_active_upstream "${ORIGINAL_UPSTREAM}" \
    "${STATE_DIR}/caddy-rollback-verified.json" || failed=1
  if [[ "${failed}" -ne 0 ]]; then
    warn "CRITICAL: rollback verification failed"
    return 86
  fi
  ROLLBACK_ARMED=false
  log "ROLLBACK VERIFIED: active upstream is ${ORIGINAL_UPSTREAM}"
}

handle_transaction_error() {
  local status="$1"
  trap - ERR
  set +e
  if [[ "${ROLLBACK_ARMED:-false}" == "true" ]]; then
    rollback_deployment
    local rollback_status=$?
    if [[ "${rollback_status}" -ne 0 ]]; then
      exit 86
    fi
  fi
  if ! quarantine_candidate; then
    warn "CRITICAL: failed candidate could not be stopped"
    exit 87
  fi
  exit "${status}"
}

handle_transaction_signal() {
  local status="$1"
  trap - ERR INT TERM
  set +e
  if [[ "${ROLLBACK_ARMED:-false}" == "true" ]]; then
    rollback_deployment
    local rollback_status=$?
    if [[ "${rollback_status}" -ne 0 ]]; then
      exit 86
    fi
  fi
  if ! quarantine_candidate; then
    warn "CRITICAL: failed candidate could not be stopped"
    exit 87
  fi
  exit "${status}"
}

run_transaction() {
  local active_host
  local direct_base_url

  require_commands
  [[ -f "${CADDYFILE}" ]] || {
    printf 'Caddyfile not found: %s\n' "${CADDYFILE}" >&2
    return 1
  }
  initialize_state
  log "Stage 1/7: snapshot active Caddy configuration"
  CADDY_BEFORE_JSON="${STATE_DIR}/caddy-before.json"
  CADDYFILE_BACKUP="${STATE_DIR}/Caddyfile.before"
  snapshot_caddy "${CADDY_BEFORE_JSON}"
  cp "${CADDYFILE}" "${CADDYFILE_BACKUP}"
  ORIGINAL_UPSTREAM="$(extract_active_upstream "${CADDY_BEFORE_JSON}")"
  active_host="${ORIGINAL_UPSTREAM%:${APP_PORT}}"
  if [[ "${active_host}" == "${ORIGINAL_UPSTREAM}" || -z "${active_host}" ]]; then
    printf 'could not derive active container from upstream: %s\n' "${ORIGINAL_UPSTREAM}" >&2
    return 1
  fi
  if [[ -n "${ACTIVE_CONTAINER}" && "${ACTIVE_CONTAINER}" != "${active_host}" ]]; then
    printf 'active container override %s disagrees with Caddy %s\n' \
      "${ACTIVE_CONTAINER}" "${active_host}" >&2
    return 1
  fi
  ACTIVE_CONTAINER="${active_host}"
  export ACTIVE_CONTAINER
  CANDIDATE_UPSTREAM="${CANDIDATE_CONTAINER}:${APP_PORT}"
  check_caddy_mount_consistency

  log "Stage 2/7: create isolated candidate ${CANDIDATE_CONTAINER}"
  create_candidate
  wait_candidate_ready
  direct_base_url="$(candidate_base_url)"

  log "Stage 3/7: probe candidate directly at ${direct_base_url}"
  probe_health "${direct_base_url}" direct
  if [[ "${SKIP_API_SMOKE}" == "true" ]]; then
    warn "API smoke: SKIPPED BY OPERATOR"
  else
    probe_api_pair "${direct_base_url}" direct
  fi

  log "Stage 4/7: render and validate candidate Caddyfile"
  CADDY_CANDIDATE_FILE="${STATE_DIR}/Caddyfile.candidate"
  render_candidate_caddyfile "${CADDYFILE}" "${CADDY_CANDIDATE_FILE}" \
    "${ORIGINAL_UPSTREAM}" "${CANDIDATE_UPSTREAM}"
  validate_caddyfile "${CADDY_CANDIDATE_FILE}"
  CADDY_EXPECTED_JSON="${STATE_DIR}/caddy-candidate-expected.json"
  adapt_caddyfile "${CADDY_CANDIDATE_FILE}" "${CADDY_EXPECTED_JSON}"
  if [[ "$(extract_active_upstream "${CADDY_EXPECTED_JSON}")" != "${CANDIDATE_UPSTREAM}" ]]; then
    printf 'adapted candidate Caddy JSON targets the wrong upstream\n' >&2
    return 1
  fi
  CADDY_OWNED_JSON="${CADDY_EXPECTED_JSON}"
  assert_caddy_baseline_unchanged

  log "Stage 5/7: cut Caddy over through stdin"
  ROLLBACK_ARMED=true
  reload_caddyfile "${CADDY_CANDIDATE_FILE}"
  CADDY_CUTOVER_OBSERVED_JSON="${STATE_DIR}/caddy-after-cutover.json"
  assert_active_upstream "${CANDIDATE_UPSTREAM}" "${CADDY_CUTOVER_OBSERVED_JSON}"
  if ! json_configs_equal "${CADDY_EXPECTED_JSON}" "${CADDY_CUTOVER_OBSERVED_JSON}"; then
    printf 'active Caddy does not match adapted candidate\n' >&2
    return 1
  fi
  write_file_in_place "${CADDY_CANDIDATE_FILE}" "${CADDYFILE}"

  log "Stage 6/7: probe public endpoint ${PUBLIC_BASE_URL}"
  probe_health "${PUBLIC_BASE_URL}" public
  if [[ "${SKIP_API_SMOKE}" != "true" ]]; then
    probe_api_pair "${PUBLIC_BASE_URL}" public
  fi

  log "Stage 7/7: soak and finalize"
  run_soak
  assert_final_commit_state
  promote_candidate_restart_policy
  CANDIDATE_COMMITTED=true
  ROLLBACK_ARMED=false
  log "DEPLOYMENT SUCCEEDED: ${ORIGINAL_UPSTREAM} -> ${CANDIDATE_UPSTREAM}"
  log "Rollback container retained: ${ACTIVE_CONTAINER}"
}
