#!/usr/bin/env bash
set -u
set +e

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "${TEST_DIR}/.." && pwd)"
SCRIPT="${DEPLOY_DIR}/hot-deploy.sh"
FIXTURES="${TEST_DIR}/fixtures"
FAKE_BIN="${FIXTURES}/fake-bin"
TEST_ROOT="${TMPDIR:-/tmp}/sub2api-hot-deploy-tests-${$}"
PASS_COUNT=0
FAIL_COUNT=0

mkdir -p "${TEST_ROOT}"

fail() {
  printf 'not ok - %s\n' "$1" >&2
  FAIL_COUNT=$((FAIL_COUNT + 1))
}

pass() {
  printf 'ok - %s\n' "$1"
  PASS_COUNT=$((PASS_COUNT + 1))
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  case "${haystack}" in
    *"${needle}"*) pass "${message}" ;;
    *) fail "${message}: expected output to contain '${needle}', got '${haystack}'" ;;
  esac
}

assert_not_contains() {
  local haystack="$1"
  local needle="$2"
  local message="$3"
  case "${haystack}" in
    *"${needle}"*) fail "${message}: output exposed '${needle}'" ;;
    *) pass "${message}" ;;
  esac
}

assert_status() {
  local actual="$1"
  local expected="$2"
  local message="$3"
  if [[ "${actual}" == "${expected}" ]]; then
    pass "${message}"
  else
    fail "${message}: expected status ${expected}, got ${actual}"
  fi
}

assert_file_contains() {
  local file="$1"
  local needle="$2"
  local message="$3"
  if [[ -f "${file}" ]] && grep -Fq -- "${needle}" "${file}"; then
    pass "${message}"
  else
    fail "${message}: '${needle}' not found in ${file}"
  fi
}

assert_file_not_contains() {
  local file="$1"
  local needle="$2"
  local message="$3"
  if [[ -f "${file}" ]] && grep -Fq -- "${needle}" "${file}"; then
    fail "${message}: '${needle}' unexpectedly found in ${file}"
  else
    pass "${message}"
  fi
}

run_deploy() {
  local case_dir="$1"
  shift
  mkdir -p "${case_dir}/state"
  env \
    STATE_DIR="${case_dir}/state" \
    LOCK_FILE="${case_dir}/deploy.lock" \
    CADDYFILE="${FIXTURES}/Caddyfile" \
    PUBLIC_BASE_URL="https://api.example.test" \
    bash "${SCRIPT}" "$@" >"${case_dir}/stdout" 2>"${case_dir}/stderr"
}

run_transaction_deploy() {
  local case_dir="$1"
  shift
  mkdir -p "${case_dir}/state"
  cp "${TRANSACTION_HOST_CADDYFILE:-${FIXTURES}/Caddyfile}" "${case_dir}/Caddyfile"
  cp "${FIXTURES}/caddy-active-v5.json" "${case_dir}/caddy-active.json"
  : >"${case_dir}/docker.log"
  : >"${case_dir}/curl.log"
  : >"${case_dir}/stdin.capture"
  env \
    PATH="${FAKE_BIN}:${PATH}" \
    STATE_DIR="${case_dir}/state" \
    LOCK_FILE="${case_dir}/deploy.lock" \
    CADDYFILE="${case_dir}/Caddyfile" \
    PUBLIC_BASE_URL="https://api.example.test" \
    CADDY_CONTAINER="sub2api-caddy" \
    FAKE_DOCKER_LOG="${case_dir}/docker.log" \
    FAKE_CURL_LOG="${case_dir}/curl.log" \
    FAKE_STDIN_CAPTURE="${case_dir}/stdin.capture" \
    FAKE_CADDY_JSON="${case_dir}/caddy-active.json" \
    FAKE_CADDY_STATE="${case_dir}/caddy-active.json" \
    FAKE_CADDY_V5="${FIXTURES}/caddy-active-v5.json" \
    FAKE_CADDY_V6="${FIXTURES}/caddy-active-v6.json" \
    FAKE_CADDY_V7="${FIXTURES}/caddy-active-v7.json" \
    FAKE_CADDY_V5_MUTATED="${FIXTURES}/caddy-active-v5-mutated.json" \
    FAKE_CADDY_V6_MUTATED="${FIXTURES}/caddy-active-v6-mutated.json" \
    FAKE_HOST_CADDYFILE="${TRANSACTION_MOUNTED_CADDYFILE:-${case_dir}/Caddyfile}" \
    SMOKE_API_KEY="production-smoke-secret" \
    HEALTH_TIMEOUT_SECONDS=2 \
    HEALTH_POLL_INTERVAL_SECONDS=1 \
    REQUEST_TIMEOUT_SECONDS=2 \
    COMPACT_SMOKE_BYTES=1024 \
    SOAK_SECONDS="${SOAK_SECONDS:-0}" \
    SOAK_INTERVAL_SECONDS=1 \
    bash "${SCRIPT}" --image example/sub2api:test --candidate sub2api-next-v6 "$@" \
    >"${case_dir}/stdout" 2>"${case_dir}/stderr"
}

captured_output() {
  local case_dir="$1"
  cat "${case_dir}/stdout" "${case_dir}/stderr" 2>/dev/null
}

test_requires_image() {
  local case_dir="${TEST_ROOT}/requires-image"
  run_deploy "${case_dir}" --skip-api-smoke --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 2 "missing image is rejected"
  assert_contains "${output}" "--image is required" "missing image explains the contract"
}

test_requires_smoke_key() {
  local case_dir="${TEST_ROOT}/requires-smoke-key"
  run_deploy "${case_dir}" --image example/sub2api:test --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 2 "missing smoke key is rejected"
  assert_contains "${output}" "SMOKE_API_KEY is required" "missing smoke key explains the production gate"
}

test_explicit_smoke_skip_is_visible() {
  local case_dir="${TEST_ROOT}/skip-smoke"
  run_deploy "${case_dir}" --image example/sub2api:test --skip-api-smoke --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "explicit smoke skip is accepted"
  assert_contains "${output}" "API smoke: SKIPPED BY OPERATOR" "smoke skip is auditable"
}

test_explicit_native_search_skip_is_visible() {
  local case_dir="${TEST_ROOT}/skip-native-search-smoke"
  SMOKE_API_KEY="native-search-skip-test-key" \
    run_deploy "${case_dir}" --image example/sub2api:test --skip-native-search-smoke --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "explicit native Search smoke skip is accepted"
  assert_contains "${output}" "API smoke: required" "Responses and Compact smoke remain required"
  assert_contains "${output}" "Native Search smoke: SKIPPED BY OPERATOR" "native Search skip is auditable"
}

test_secret_is_redacted() {
  local case_dir="${TEST_ROOT}/redaction"
  local secret="compact-secret-value"
  SMOKE_API_KEY="${secret}" run_deploy "${case_dir}" --image example/sub2api:test --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "dry-run accepts environment smoke key"
  assert_not_contains "${output}" "${secret}" "stdout and stderr redact the smoke key"
  if rg -q --fixed-strings "${secret}" "${case_dir}/state" 2>/dev/null; then
    fail "state artifacts redact the smoke key"
  else
    pass "state artifacts redact the smoke key"
  fi
}

test_unknown_argument_is_rejected() {
  local case_dir="${TEST_ROOT}/unknown-argument"
  run_deploy "${case_dir}" --image example/sub2api:test --skip-api-smoke --unknown-option
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 2 "unknown argument is rejected"
  assert_contains "${output}" "unknown argument: --unknown-option" "unknown argument is named"
}

test_lock_rejection() {
  local case_dir="${TEST_ROOT}/lock"
  mkdir -p "${case_dir}"
  : >"${case_dir}/deploy.lock.held"
  FAKE_LOCK_HELD=1 run_deploy "${case_dir}" \
    --image example/sub2api:test \
    --skip-api-smoke \
    --dry-run
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 3 "concurrent deployment is rejected"
  assert_contains "${output}" "another deployment holds the lock" "lock rejection is explicit"
}

run_config_tests() {
  test_requires_image
  test_requires_smoke_key
  test_explicit_smoke_skip_is_visible
  test_explicit_native_search_skip_is_visible
  test_secret_is_redacted
  test_unknown_argument_is_rejected
  test_lock_rejection
}

setup_caddy_case() {
  local case_dir="$1"
  mkdir -p "${case_dir}"
  : >"${case_dir}/docker.log"
  : >"${case_dir}/stdin.capture"
  export PATH="${FAKE_BIN}:${PATH}"
  export FAKE_DOCKER_LOG="${case_dir}/docker.log"
  export FAKE_STDIN_CAPTURE="${case_dir}/stdin.capture"
  export FAKE_CADDY_JSON="${FIXTURES}/caddy-active-v5.json"
  export FAKE_CADDY_V5="${FIXTURES}/caddy-active-v5.json"
  export FAKE_CADDY_V6="${FIXTURES}/caddy-active-v6.json"
  export CADDY_CONTAINER="sub2api-caddy"
  export CADDY_ADMIN_URL="http://127.0.0.1:2019/config/"
  export APP_PORT=8080
  # shellcheck source=../lib/hot-deploy-common.sh
  source "${DEPLOY_DIR}/lib/hot-deploy-common.sh"
}

setup_candidate_case() {
  local case_dir="$1"
  setup_caddy_case "${case_dir}"
  : >"${case_dir}/curl.log"
  export FAKE_CURL_LOG="${case_dir}/curl.log"
  export IMAGE="example/sub2api:test"
  export ACTIVE_CONTAINER="sub2api-next-v5"
  export CANDIDATE_CONTAINER="sub2api-next-v6"
  export DOCKER_NETWORK=""
  export STATE_DIR="${case_dir}/state"
  export HEALTH_TIMEOUT_SECONDS=2
  export HEALTH_POLL_INTERVAL_SECONDS=1
  export REQUEST_TIMEOUT_SECONDS=2
  export COMPACT_SMOKE_BYTES=1024
  export HEALTH_PATH=/health
  export RESPONSES_PATH=/v1/responses
  export COMPACT_PATH=/responses
  export NATIVE_SEARCH_ROOT_PATH=/alpha/search
  export NATIVE_SEARCH_V1_PATH=/v1/alpha/search
  export SMOKE_MODEL=gpt-5.6-sol
  export SMOKE_USER_AGENT=codex_cli_rs/hot-deploy
  export SMOKE_ORIGINATOR=codex_cli_rs
  export SMOKE_API_KEY=production-smoke-secret
  mkdir -p "${STATE_DIR}"
}

test_extracts_active_caddy_upstream() {
  local actual
  actual="$(extract_active_upstream "${FIXTURES}/caddy-active-v5.json")"
  if [[ "${actual}" == "sub2api-next-v5:8080" ]]; then
    pass "active upstream comes from Caddy JSON"
  else
    fail "active upstream comes from Caddy JSON: got '${actual}'"
  fi
}

test_rejects_ambiguous_active_upstreams() {
  local output
  output="$(extract_active_upstream "${FIXTURES}/caddy-active-multiple.json" 2>&1)"
  local status=$?
  assert_status "${status}" 1 "multiple active upstreams are rejected"
  assert_contains "${output}" "expected exactly one" "ambiguous upstream failure is explicit"
}

test_renders_candidate_from_discovered_upstream() {
  local case_dir="${TEST_ROOT}/caddy-render"
  local candidate="${case_dir}/candidate.Caddyfile"
  mkdir -p "${case_dir}"
  render_candidate_caddyfile \
    "${FIXTURES}/Caddyfile" \
    "${candidate}" \
    "sub2api-next-v5:8080" \
    "sub2api-next-v6:8080"
  assert_file_contains "${candidate}" "reverse_proxy sub2api-next-v6:8080" "candidate Caddyfile targets the new container"
  if grep -Fq -- "sub2api-next-v5:8080" "${candidate}"; then
    fail "candidate Caddyfile removes the old upstream"
  else
    pass "candidate Caddyfile removes the old upstream"
  fi
}

test_caddy_operations_use_stdin_only() {
  local case_dir="${TEST_ROOT}/caddy-stdin"
  setup_caddy_case "${case_dir}"
  validate_caddyfile "${FIXTURES}/Caddyfile"
  adapt_caddyfile "${FIXTURES}/Caddyfile" "${case_dir}/adapted.json"
  reload_caddyfile "${FIXTURES}/Caddyfile"
  assert_file_contains "${case_dir}/stdin.capture" "sub2api-next-v5:8080" "Caddy receives candidate configuration over stdin"
  if grep -Fq -- "/etc/caddy/Caddyfile" "${case_dir}/docker.log"; then
    fail "Caddy operations never read the container-mounted Caddyfile"
  else
    pass "Caddy operations never read the container-mounted Caddyfile"
  fi
  assert_file_contains "${case_dir}/docker.log" "--config - --adapter caddyfile" "Caddyfile reload selects stdin and the caddyfile adapter"
  assert_file_contains "${case_dir}/adapted.json" "sub2api-next-v5:8080" "Caddy adapt produces the expected native JSON over stdin"
}

test_snapshot_and_active_assertion() {
  local case_dir="${TEST_ROOT}/caddy-active-assert"
  setup_caddy_case "${case_dir}"
  FAKE_CADDY_JSON="${FIXTURES}/caddy-active-v6.json"
  export FAKE_CADDY_JSON
  snapshot_caddy "${case_dir}/active.json"
  assert_active_upstream "sub2api-next-v6:8080" "${case_dir}/verified.json"
  assert_file_contains "${case_dir}/verified.json" "sub2api-next-v6:8080" "active assertion records verified Caddy JSON"
}

test_restore_uses_native_json_stdin() {
  local case_dir="${TEST_ROOT}/caddy-restore"
  setup_caddy_case "${case_dir}"
  restore_caddy_json "${FIXTURES}/caddy-active-v5.json"
  assert_file_contains "${case_dir}/stdin.capture" '"dial": "sub2api-next-v5:8080"' "rollback sends saved native Caddy JSON"
  assert_file_contains "${case_dir}/docker.log" "caddy reload --config -" "rollback reloads native JSON from stdin"
  if grep -Fq -- "--adapter" "${case_dir}/docker.log"; then
    fail "native JSON rollback does not use a Caddyfile adapter"
  else
    pass "native JSON rollback does not use a Caddyfile adapter"
  fi
}

test_stale_recovery_json_comparison_normalizes_only_caddy_source_hide_path() {
  local case_dir="${TEST_ROOT}/caddy-stale-recovery-json"
  mkdir -p "${case_dir}"
  cat >"${case_dir}/stdin.json" <<'JSON'
{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"file_server","hide":["./-"]},{"handler":"reverse_proxy","upstreams":[{"dial":"sub2api-next-v5:8080"}]}]}]}}}}}
JSON
  cat >"${case_dir}/mounted.json" <<'JSON'
{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"file_server","hide":["/etc/caddy/Caddyfile"]},{"handler":"reverse_proxy","upstreams":[{"dial":"sub2api-next-v5:8080"}]}]}]}}}}}
JSON
  cat >"${case_dir}/external.json" <<'JSON'
{"apps":{"http":{"servers":{"srv0":{"listen":[":443"],"routes":[{"handle":[{"handler":"file_server","hide":["/etc/caddy/Caddyfile"]},{"handler":"reverse_proxy","upstreams":[{"dial":"sub2api-next-v5:8080"}]}]}]}}}}}
JSON

  if json_configs_equal_for_stale_recovery "${case_dir}/stdin.json" "${case_dir}/mounted.json"; then
    pass "stale recovery treats Caddy source hide paths as equivalent"
  else
    fail "stale recovery treats Caddy source hide paths as equivalent"
  fi
  if json_configs_equal_for_stale_recovery "${case_dir}/stdin.json" "${case_dir}/external.json"; then
    fail "stale recovery still rejects unrelated JSON drift"
  else
    pass "stale recovery still rejects unrelated JSON drift"
  fi
}

run_caddy_tests() {
  setup_caddy_case "${TEST_ROOT}/caddy-setup"
  test_extracts_active_caddy_upstream
  test_rejects_ambiguous_active_upstreams
  test_renders_candidate_from_discovered_upstream
  test_caddy_operations_use_stdin_only
  test_snapshot_and_active_assertion
  test_restore_uses_native_json_stdin
  test_stale_recovery_json_comparison_normalizes_only_caddy_source_hide_path
}

test_candidate_clones_runtime_without_ports() {
  local case_dir="${TEST_ROOT}/candidate-clone"
  setup_candidate_case "${case_dir}"
  create_candidate
  assert_file_contains "${case_dir}/docker.log" "create --name sub2api-next-v6" "candidate is created with the requested name"
  assert_file_contains "${case_dir}/docker.log" "--network deploy_sub2api-network" "candidate joins the active Docker network"
  assert_file_contains "${case_dir}/docker.log" "--volumes-from sub2api-next-v5" "candidate inherits application volumes"
  assert_file_contains "${case_dir}/docker.log" "--restart no" "candidate cannot restart before validation commits"
  assert_file_not_contains "${case_dir}/docker.log" "create --name sub2api-next-v6 --restart unless-stopped" "candidate does not inherit restart policy before validation"
  assert_file_contains "${case_dir}/docker.log" "--ulimit nofile=100000:100000" "candidate inherits ulimits"
  assert_file_contains "${case_dir}/docker.log" "--env DATABASE_PASSWORD" "candidate references inherited environment by name"
  assert_file_contains "${case_dir}/docker.log" "inherited-env-present=DATABASE_PASSWORD" "candidate receives the inherited secret through process environment"
  assert_file_not_contains "${case_dir}/docker.log" "production-secret" "candidate secrets never enter docker argv"
  if grep -Eq -- '(^| )(--publish|-p)( |$)' "${case_dir}/docker.log"; then
    fail "candidate does not publish a host port"
  else
    pass "candidate does not publish a host port"
  fi
}

test_unhealthy_candidate_is_rejected() {
  local case_dir="${TEST_ROOT}/candidate-unhealthy"
  setup_candidate_case "${case_dir}"
  FAKE_CANDIDATE_HEALTH=unhealthy wait_candidate_ready 2>"${case_dir}/wait.err"
  local status=$?
  assert_status "${status}" 1 "unhealthy candidate is rejected"
  assert_file_contains "${case_dir}/wait.err" "reported unhealthy" "unhealthy state is explicit"
}

test_starting_candidate_times_out() {
  local case_dir="${TEST_ROOT}/candidate-timeout"
  setup_candidate_case "${case_dir}"
  HEALTH_TIMEOUT_SECONDS=1 FAKE_CANDIDATE_HEALTH=starting wait_candidate_ready 2>"${case_dir}/wait.err"
  local status=$?
  assert_status "${status}" 1 "candidate health wait has a deadline"
  assert_file_contains "${case_dir}/wait.err" "timed out" "candidate timeout is explicit"
}

test_responses_failure_blocks_candidate() {
  local case_dir="${TEST_ROOT}/candidate-responses-fail"
  setup_candidate_case "${case_dir}"
  FAKE_RESPONSES_FAIL=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Responses failure rejects candidate"
}

test_responses_requires_real_response_shape() {
  local case_dir="${TEST_ROOT}/candidate-responses-invalid"
  setup_candidate_case "${case_dir}"
  FAKE_RESPONSES_INVALID=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Responses smoke rejects an empty JSON object"
}

test_responses_requires_completed_status() {
  local case_dir="${TEST_ROOT}/candidate-responses-status-missing"
  setup_candidate_case "${case_dir}"
  FAKE_RESPONSES_STATUS_MISSING=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Responses smoke rejects a missing completion status"
}

test_responses_requires_canary_output() {
  local case_dir="${TEST_ROOT}/candidate-responses-empty-output"
  setup_candidate_case "${case_dir}"
  FAKE_RESPONSES_EMPTY_OUTPUT=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Responses smoke rejects empty output"
}

test_compact_failure_blocks_candidate() {
  local case_dir="${TEST_ROOT}/candidate-compact-fail"
  setup_candidate_case "${case_dir}"
  FAKE_COMPACT_FAIL=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Compact failure rejects candidate"
  assert_file_contains "${case_dir}/curl.log" "kind=compact" "candidate gate actually calls Compact"
}

test_compact_requires_terminal_compaction_sse() {
  local case_dir="${TEST_ROOT}/candidate-compact-invalid"
  setup_candidate_case "${case_dir}"
  FAKE_COMPACT_INVALID=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Compact smoke rejects a non-compaction response"
}

test_compact_requires_completed_status() {
  local case_dir="${TEST_ROOT}/candidate-compact-incomplete"
  setup_candidate_case "${case_dir}"
  FAKE_COMPACT_INCOMPLETE=1 probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "Compact smoke rejects an incomplete terminal response"
}

test_compact_matched_upstream_degradation_allows_candidate() {
  local case_dir="${TEST_ROOT}/candidate-compact-matched-upstream-degradation"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_COMPACT_HTTP_STATUS=502 \
    FAKE_BASELINE_COMPACT_HTTP_STATUS=503 \
    probe_compact_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 0 "matching candidate/active upstream degradation does not require a fresh OAuth account"
  assert_file_contains "${case_dir}/probe.err" "MATCHED UPSTREAM DEGRADATION" "matched degradation is explicit and auditable"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=compact" "candidate Compact is still exercised"
  assert_file_contains "${case_dir}/curl.log" "scope=direct-active-baseline kind=compact" "active Compact baseline is exercised"
}

test_compact_candidate_only_failure_still_blocks() {
  local case_dir="${TEST_ROOT}/candidate-compact-candidate-only-failure"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_COMPACT_HTTP_STATUS=502 \
    probe_compact_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "candidate-only Compact failure remains blocking"
}

test_compact_non_upstream_failure_still_blocks() {
  local case_dir="${TEST_ROOT}/candidate-compact-auth-failure"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_COMPACT_HTTP_STATUS=401 \
    FAKE_BASELINE_COMPACT_HTTP_STATUS=401 \
    probe_compact_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "matched authentication failure remains blocking"
}

test_native_search_matched_upstream_degradation_allows_candidate() {
  local case_dir="${TEST_ROOT}/candidate-native-search-matched-upstream-degradation"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_NATIVE_SEARCH_HTTP_STATUS=503 \
    FAKE_BASELINE_NATIVE_SEARCH_HTTP_STATUS=503 \
    probe_native_search_pair_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 0 "matching candidate/active native Search degradation does not require a fresh OAuth account"
  assert_file_contains "${case_dir}/probe.err" "MATCHED NATIVE SEARCH UPSTREAM DEGRADATION" "matched native Search degradation is explicit and auditable"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=native-search-root" "candidate native Search is still exercised"
  assert_file_contains "${case_dir}/curl.log" "scope=direct-active-baseline kind=native-search-root" "active native Search baseline is exercised"
}

test_native_search_candidate_only_failure_still_blocks() {
  local case_dir="${TEST_ROOT}/candidate-native-search-candidate-only-failure"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_NATIVE_SEARCH_HTTP_STATUS=503 \
    probe_native_search_pair_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "candidate-only native Search failure remains blocking"
}

test_native_search_non_upstream_failure_still_blocks() {
  local case_dir="${TEST_ROOT}/candidate-native-search-auth-failure"
  setup_candidate_case "${case_dir}"
  FAKE_DIRECT_NATIVE_SEARCH_HTTP_STATUS=401 \
    FAKE_BASELINE_NATIVE_SEARCH_HTTP_STATUS=401 \
    probe_native_search_pair_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 1 "matched native Search authentication failure remains blocking"
}

test_candidate_probe_pair_succeeds_without_secret_artifacts() {
  local case_dir="${TEST_ROOT}/candidate-probes-pass"
  setup_candidate_case "${case_dir}"
  local base_url
  base_url="$(candidate_base_url)"
  probe_health "${base_url}" direct
  probe_api_pair "${base_url}" direct
  local status=$?
  assert_status "${status}" 0 "candidate health, Responses, Compact, and native Search probes pass"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=health" "direct health probe is recorded"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=responses" "direct Responses probe is recorded"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=compact" "direct Compact probe is recorded"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=native-search-root" "direct root native Search probe is recorded"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=native-search-v1" "direct v1 native Search probe is recorded"
  assert_file_contains "${case_dir}/curl.log" 'header = "User-Agent: codex_cli_rs/' "smoke identifies as an official Codex client"
  assert_file_contains "${case_dir}/curl.log" 'header = "originator: codex_cli_rs"' "smoke sends the paired Codex originator"
  assert_file_contains "${COMPACT_SMOKE_PAYLOAD}" '"stream":true' "Compact smoke exercises the streaming client path"
  assert_file_contains "${COMPACT_SMOKE_PAYLOAD}" '"type":"compaction_trigger"' "Compact smoke exercises remote compact v2 body promotion"
  if rg -q --fixed-strings "${SMOKE_API_KEY}" "${STATE_DIR}" "${case_dir}/curl.log" 2>/dev/null; then
    fail "candidate probe artifacts redact the API key"
  else
    pass "candidate probe artifacts redact the API key"
  fi
  if rg -q 'SEARCH_(OUTPUT|ENCRYPTED)_SENTINEL' "${STATE_DIR}" "${case_dir}/curl.log" 2>/dev/null; then
    fail "candidate probe artifacts must not retain native Search output"
  else
    pass "candidate probe artifacts do not retain native Search output"
  fi
}

test_candidate_probe_pair_can_skip_only_native_search() {
  local case_dir="${TEST_ROOT}/candidate-probes-skip-native-search"
  setup_candidate_case "${case_dir}"
  SKIP_NATIVE_SEARCH_SMOKE=true \
    probe_api_pair_with_active_baseline \
      "http://172.18.0.99:8080" direct "http://172.18.0.98:8080" \
      >"${case_dir}/probe.out" 2>"${case_dir}/probe.err"
  local status=$?
  assert_status "${status}" 0 "candidate may skip only native Search when explicitly authorized"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=responses" "Responses probe still runs"
  assert_file_contains "${case_dir}/curl.log" "scope=direct kind=compact" "Compact probe still runs"
  assert_file_not_contains "${case_dir}/curl.log" "kind=native-search" "native Search probes are skipped"
  assert_file_contains "${case_dir}/probe.err" "Native Search smoke: SKIPPED BY OPERATOR" "native Search skip is recorded"
}

test_native_search_rejects_invalid_contracts() {
  local case_dir mode status env_var
  for mode in TRANSPORT_FAIL HTML BAD_CONTENT_TYPE MALFORMED MISSING_OUTPUT INVALID_OUTPUT INVALID_ENCRYPTED; do
    case_dir="${TEST_ROOT}/candidate-native-search-${mode}"
    setup_candidate_case "${case_dir}"
    env_var="FAKE_NATIVE_SEARCH_${mode}"
    printf -v "${env_var}" '%s' 1
    export "${env_var}"
    probe_api_pair "http://172.18.0.99:8080" direct 2>"${case_dir}/probe.err"
    status=$?
    unset "${env_var}"
    assert_status "${status}" 1 "native Search rejects ${mode} response"
  done
}

run_candidate_tests() {
  test_candidate_clones_runtime_without_ports
  test_unhealthy_candidate_is_rejected
  test_starting_candidate_times_out
  test_responses_failure_blocks_candidate
  test_responses_requires_real_response_shape
  test_responses_requires_completed_status
  test_responses_requires_canary_output
  test_compact_failure_blocks_candidate
  test_compact_requires_terminal_compaction_sse
  test_compact_requires_completed_status
  test_compact_matched_upstream_degradation_allows_candidate
  test_compact_candidate_only_failure_still_blocks
  test_compact_non_upstream_failure_still_blocks
  test_native_search_matched_upstream_degradation_allows_candidate
  test_native_search_candidate_only_failure_still_blocks
  test_native_search_non_upstream_failure_still_blocks
  test_candidate_probe_pair_succeeds_without_secret_artifacts
  test_candidate_probe_pair_can_skip_only_native_search
  test_native_search_rejects_invalid_contracts
}

test_transaction_success_commits_cutover() {
  local case_dir="${TEST_ROOT}/transaction-success"
  run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "complete transaction succeeds"
  assert_contains "${output}" "DEPLOYMENT SUCCEEDED" "success is declared only at the end"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v6:8080" "active Caddy JSON targets candidate after success"
  assert_file_contains "${case_dir}/Caddyfile" "sub2api-next-v6:8080" "persistent Caddyfile targets candidate after success"
  assert_file_not_contains "${case_dir}/docker.log" " stop sub2api-next-v5" "success does not stop the rollback container"
  assert_file_not_contains "${case_dir}/docker.log" " rm sub2api-next-v5" "success does not remove the rollback container"
  assert_file_contains "${case_dir}/docker.log" "update --restart unless-stopped sub2api-next-v6" "success promotes candidate restart policy only after final validation"
}

test_transaction_matched_compact_degradation_commits_cutover() {
  local case_dir="${TEST_ROOT}/transaction-matched-compact-degradation"
  FAKE_DIRECT_COMPACT_HTTP_STATUS=502 \
    FAKE_PUBLIC_COMPACT_HTTP_STATUS=524 \
    FAKE_BASELINE_COMPACT_HTTP_STATUS=503 \
    run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "matched Compact upstream degradation can complete a deployment"
  assert_contains "${output}" "MATCHED UPSTREAM DEGRADATION" "degraded deployment records the baseline comparison"
  assert_contains "${output}" "DEPLOYMENT SUCCEEDED" "degraded deployment still requires final commit"
  assert_file_contains "${case_dir}/curl.log" "scope=direct-active-baseline kind=compact" "direct Compact compares the old production baseline"
  assert_file_contains "${case_dir}/curl.log" "scope=public-active-baseline kind=compact" "public Compact compares the old production baseline"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v6:8080" "matched degradation commits the candidate"
}

test_transaction_matched_native_search_degradation_commits_cutover() {
  local case_dir="${TEST_ROOT}/transaction-matched-native-search-degradation"
  FAKE_DIRECT_NATIVE_SEARCH_HTTP_STATUS=503 \
    FAKE_PUBLIC_NATIVE_SEARCH_HTTP_STATUS=503 \
    FAKE_BASELINE_NATIVE_SEARCH_HTTP_STATUS=503 \
    run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "matched native Search upstream degradation can complete a deployment"
  assert_contains "${output}" "MATCHED NATIVE SEARCH UPSTREAM DEGRADATION" "native Search degradation records the baseline comparison"
  assert_contains "${output}" "DEPLOYMENT SUCCEEDED" "native Search degraded deployment still requires final commit"
  assert_file_contains "${case_dir}/curl.log" "scope=direct-active-baseline kind=native-search-root" "direct native Search compares the old production baseline"
  assert_file_contains "${case_dir}/curl.log" "scope=public-active-baseline kind=native-search-root" "public native Search compares the old production baseline"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v6:8080" "matched native Search degradation commits the candidate"
}

test_stale_host_caddyfile_requires_explicit_recovery() {
  local case_dir="${TEST_ROOT}/transaction-stale-host-default"
  mkdir -p "${case_dir}"
  cp "${FIXTURES}/Caddyfile" "${case_dir}/mounted.Caddyfile"
  sed 's/sub2api-next-v5:8080/sub2api:8080/' "${FIXTURES}/Caddyfile" >"${case_dir}/stale.Caddyfile"
  TRANSACTION_HOST_CADDYFILE="${case_dir}/stale.Caddyfile" \
    TRANSACTION_MOUNTED_CADDYFILE="${case_dir}/mounted.Caddyfile" \
    run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "stale host Caddyfile blocks deployment by default"
  else
    fail "stale host Caddyfile blocks deployment by default"
  fi
  assert_contains "${output}" "host and mounted Caddyfile differ" "stale bind inode is reported"
  assert_file_not_contains "${case_dir}/Caddyfile" "sub2api-next-v6:8080" "default mode does not overwrite stale host Caddyfile"
}

test_explicit_stale_host_recovery_requires_matching_active_json() {
  local case_dir="${TEST_ROOT}/transaction-stale-host-mismatch"
  mkdir -p "${case_dir}"
  cp "${FIXTURES}/Caddyfile" "${case_dir}/mounted.Caddyfile"
  sed 's/sub2api-next-v5:8080/sub2api:8080/' "${FIXTURES}/Caddyfile" >"${case_dir}/stale.Caddyfile"
  TRANSACTION_HOST_CADDYFILE="${case_dir}/stale.Caddyfile" \
    TRANSACTION_MOUNTED_CADDYFILE="${case_dir}/mounted.Caddyfile" \
    FAKE_CADDY_ADAPT_MISMATCH=1 \
    run_transaction_deploy "${case_dir}" --recover-stale-host-caddyfile
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "stale host recovery rejects mismatched active JSON"
  else
    fail "stale host recovery rejects mismatched active JSON"
  fi
  assert_contains "${output}" "mounted Caddyfile does not match active Caddy JSON" "recovery mismatch is explicit"
  assert_file_not_contains "${case_dir}/Caddyfile" "sub2api-next-v5:8080" "mismatched recovery leaves host Caddyfile untouched"
}

test_explicit_stale_host_recovery_commits_transaction() {
  local case_dir="${TEST_ROOT}/transaction-stale-host-recovery"
  mkdir -p "${case_dir}"
  cp "${FIXTURES}/Caddyfile" "${case_dir}/mounted.Caddyfile"
  sed 's/sub2api-next-v5:8080/sub2api:8080/' "${FIXTURES}/Caddyfile" >"${case_dir}/stale.Caddyfile"
  TRANSACTION_HOST_CADDYFILE="${case_dir}/stale.Caddyfile" \
    TRANSACTION_MOUNTED_CADDYFILE="${case_dir}/mounted.Caddyfile" \
    run_transaction_deploy "${case_dir}" --recover-stale-host-caddyfile
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 0 "verified stale host recovery completes deployment"
  assert_contains "${output}" "STALE HOST CADDYFILE RECOVERED" "verified recovery is auditable"
  assert_contains "${output}" "DEPLOYMENT SUCCEEDED" "recovered transaction reaches final commit"
  assert_file_contains "${case_dir}/Caddyfile" "sub2api-next-v6:8080" "recovered host Caddyfile advances to candidate"
}

test_public_failure_rolls_back() {
  local case_dir="${TEST_ROOT}/transaction-public-fail"
  FAKE_PUBLIC_FAIL=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "public probe failure fails the deployment"
  else
    fail "public probe failure fails the deployment"
  fi
  assert_contains "${output}" "ROLLBACK VERIFIED" "public failure verifies rollback"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v5:8080" "rollback restores active Caddy JSON"
  assert_file_contains "${case_dir}/Caddyfile" "sub2api-next-v5:8080" "rollback restores persistent Caddyfile"
}

test_public_native_search_failure_rolls_back() {
  local case_dir="${TEST_ROOT}/transaction-public-native-search-fail"
  FAKE_PUBLIC_NATIVE_SEARCH_V1_FAIL=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "public native Search failure fails the deployment"
  else
    fail "public native Search failure fails the deployment"
  fi
  assert_contains "${output}" "ROLLBACK VERIFIED" "public native Search failure verifies rollback"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v5:8080" "native Search rollback restores active Caddy JSON"
}

test_soak_failure_rolls_back() {
  local case_dir="${TEST_ROOT}/transaction-soak-fail"
  SOAK_SECONDS=2 FAKE_PUBLIC_HEALTH_FAIL_AFTER=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "soak health failure fails the deployment"
  else
    fail "soak health failure fails the deployment"
  fi
  assert_contains "${output}" "ROLLBACK VERIFIED" "soak failure verifies rollback"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v5:8080" "soak rollback restores active upstream"
}

test_rollback_verification_failure_is_critical() {
  local case_dir="${TEST_ROOT}/transaction-rollback-critical"
  FAKE_PUBLIC_FAIL=1 FAKE_ROLLBACK_VERIFY_FAIL=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 86 "rollback verification failure has a distinct exit status"
  assert_contains "${output}" "CRITICAL: rollback verification failed" "rollback failure is unmistakable"
}

test_pre_cutover_caddy_drift_aborts_without_overwrite() {
  local case_dir="${TEST_ROOT}/transaction-pre-cutover-drift"
  FAKE_CADDY_DRIFT_AFTER_DIRECT=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "pre-cutover Caddy drift aborts deployment"
  else
    fail "pre-cutover Caddy drift aborts deployment"
  fi
  assert_contains "${output}" "active Caddy configuration changed before cutover" "pre-cutover compare-and-swap failure is explicit"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-external-v7:8080" "pre-cutover drift is not overwritten"
  assert_file_not_contains "${case_dir}/docker.log" "caddy reload --config - --adapter caddyfile" "pre-cutover drift prevents Caddy reload"
}

test_pre_cutover_same_upstream_json_drift_aborts() {
  local case_dir="${TEST_ROOT}/transaction-pre-cutover-json-drift"
  FAKE_CADDY_JSON_DRIFT_AFTER_DIRECT=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  if [[ "${status}" -ne 0 ]]; then
    pass "same-upstream Caddy JSON drift aborts deployment"
  else
    fail "same-upstream Caddy JSON drift aborts deployment"
  fi
  assert_contains "${output}" "active Caddy configuration changed before cutover" "full Caddy JSON compare catches same-upstream drift"
  assert_file_contains "${case_dir}/caddy-active.json" '"listen": [":443"]' "same-upstream external Caddy JSON remains active"
  assert_file_not_contains "${case_dir}/docker.log" "caddy reload --config - --adapter caddyfile" "same-upstream drift prevents reload"
}

test_post_cutover_caddy_drift_is_not_rolled_over() {
  local case_dir="${TEST_ROOT}/transaction-post-cutover-drift"
  FAKE_CADDY_DRIFT_AFTER_PUBLIC_COMPACT=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 86 "lost Caddy ownership has a critical exit status"
  assert_contains "${output}" "CRITICAL: rollback ownership lost" "external post-cutover change is explicit"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-external-v7:8080" "rollback does not overwrite an external Caddy change"
}

test_post_cutover_same_upstream_json_drift_is_not_overwritten() {
  local case_dir="${TEST_ROOT}/transaction-post-cutover-json-drift"
  FAKE_CADDY_JSON_DRIFT_AFTER_PUBLIC_COMPACT=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 86 "same-upstream ownership loss has a critical exit status"
  assert_contains "${output}" "CRITICAL: rollback ownership lost" "same-upstream external Caddy change is explicit"
  assert_file_contains "${case_dir}/caddy-active.json" '"listen": [":443"]' "rollback preserves same-upstream external JSON"
}

test_final_host_caddyfile_drift_rolls_back() {
  local case_dir="${TEST_ROOT}/transaction-final-host-drift"
  FAKE_HOST_CADDY_DRIFT_AFTER_PUBLIC_COMPACT=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 86 "external host Caddyfile drift loses rollback ownership"
  assert_contains "${output}" "persistent Caddyfile changed before commit" "final host drift is explicit"
  assert_contains "${output}" "CRITICAL: rollback ownership lost; host Caddyfile is external" "external host file is not overwritten"
  assert_file_contains "${case_dir}/Caddyfile" "# external host mutation" "rollback preserves external host Caddyfile"
}

test_reload_snapshot_must_match_adapted_candidate() {
  local case_dir="${TEST_ROOT}/transaction-cutover-adapt-race"
  FAKE_CADDY_DRIFT_DURING_CUTOVER=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 86 "reload snapshot mismatch loses ownership"
  assert_contains "${output}" "active Caddy does not match adapted candidate" "reload-to-snapshot race is detected"
  assert_contains "${output}" "CRITICAL: rollback ownership lost" "unowned cutover snapshot is not rolled over"
  assert_file_contains "${case_dir}/caddy-active.json" '"listen": [":443"]' "external cutover JSON remains active"
}

test_pre_cutover_failure_quarantines_candidate() {
  local case_dir="${TEST_ROOT}/transaction-candidate-quarantine"
  FAKE_RESPONSES_FAIL=1 run_transaction_deploy "${case_dir}"
  local status=$?
  if [[ "${status}" -ne 0 ]]; then
    pass "pre-cutover probe failure fails deployment"
  else
    fail "pre-cutover probe failure fails deployment"
  fi
  assert_file_contains "${case_dir}/docker.log" "stop --time 10 sub2api-next-v6" "failed candidate is stopped but retained"
  assert_file_not_contains "${case_dir}/docker.log" "rm sub2api-next-v6" "failed candidate is not removed"
}

test_quarantine_failure_is_critical() {
  local case_dir="${TEST_ROOT}/transaction-quarantine-fail"
  FAKE_RESPONSES_FAIL=1 FAKE_STOP_FAIL=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 87 "candidate stop failure has a distinct critical status"
  assert_contains "${output}" "CRITICAL: failed candidate could not be stopped" "candidate stop failure is explicit"
}

test_signal_rolls_back() {
  local case_dir="${TEST_ROOT}/transaction-signal"
  FAKE_SIGNAL_ON_PUBLIC=1 run_transaction_deploy "${case_dir}"
  local status=$?
  local output
  output="$(captured_output "${case_dir}")"
  assert_status "${status}" 143 "TERM interruption returns signal status"
  assert_contains "${output}" "ROLLBACK VERIFIED" "TERM interruption verifies rollback"
  assert_file_contains "${case_dir}/caddy-active.json" "sub2api-next-v5:8080" "TERM interruption restores active upstream"
}

run_transaction_tests() {
  test_transaction_success_commits_cutover
  test_transaction_matched_compact_degradation_commits_cutover
  test_transaction_matched_native_search_degradation_commits_cutover
  test_stale_host_caddyfile_requires_explicit_recovery
  test_explicit_stale_host_recovery_requires_matching_active_json
  test_explicit_stale_host_recovery_commits_transaction
  test_public_failure_rolls_back
  test_public_native_search_failure_rolls_back
  test_soak_failure_rolls_back
  test_rollback_verification_failure_is_critical
  test_pre_cutover_caddy_drift_aborts_without_overwrite
  test_pre_cutover_same_upstream_json_drift_aborts
  test_post_cutover_caddy_drift_is_not_rolled_over
  test_post_cutover_same_upstream_json_drift_is_not_overwritten
  test_final_host_caddyfile_drift_rolls_back
  test_reload_snapshot_must_match_adapted_candidate
  test_pre_cutover_failure_quarantines_candidate
  test_quarantine_failure_is_critical
  test_signal_rolls_back
}

test_repository_deployment_policy() {
  local agents_file="${DEPLOY_DIR}/../AGENTS.md"
  local runbook="${DEPLOY_DIR}/HOT_DEPLOY.md"
  local example_config="${DEPLOY_DIR}/hot-deploy.env.example"
  local caddyfile="${DEPLOY_DIR}/Caddyfile.zhumeng"
  assert_file_contains "${agents_file}" "deploy/hot-deploy.sh" "project policy names the only production hot-deploy entry point"
  assert_file_contains "${agents_file}" "--config /etc/caddy/Caddyfile" "project policy prohibits mounted-file reload"
  assert_file_contains "${agents_file}" "PostgreSQL" "project policy protects production databases"
  assert_file_contains "${runbook}" "SMOKE_API_KEY" "runbook documents mandatory API smoke credentials"
  assert_file_contains "${runbook}" "ROLLBACK VERIFIED" "runbook documents verified rollback evidence"
  assert_file_contains "${runbook}" "/responses" "runbook documents the real Codex remote compact path"
  assert_file_contains "${runbook}" "MATCHED UPSTREAM DEGRADATION" "runbook documents comparative upstream gating"
  assert_file_contains "${runbook}" "/alpha/search" "runbook documents native Search deployment gates"
  assert_file_contains "${runbook}" "never written" "runbook documents native Search artifact privacy"
  assert_file_contains "${example_config}" "COMPACT_SMOKE_BYTES=1048576" "example config keeps the large compact canary"
  assert_file_contains "${example_config}" "ALLOW_MATCHED_UPSTREAM_DEGRADATION=true" "example config enables comparative Compact gating"
  assert_file_contains "${DEPLOY_DIR}/Makefile" "test-hot-deploy" "deploy Makefile exposes the regression suite"
  assert_file_contains "${caddyfile}" "@ai_gateway_paths" "API hostname uses an explicit gateway allowlist"
  assert_file_contains "${caddyfile}" "path /v1/*" "API allowlist keeps versioned gateway routes"
  assert_file_contains "${caddyfile}" "path /responses*" "API allowlist keeps direct Responses routes"
  assert_file_contains "${caddyfile}" "handle @ai_gateway_paths" "only allowlisted API paths reach the application"
  assert_file_not_contains "${caddyfile}" "@workbench_paths" "API hostname no longer relies on a workbench blacklist"
  local repository_root="${DEPLOY_DIR}/.."
  local required_file
  for required_file in \
    AGENTS.md \
    deploy/tests/test-hot-deploy.sh \
    docs/superpowers/specs/2026-07-15-transactional-hot-deploy.md \
    docs/superpowers/plans/2026-07-15-transactional-hot-deploy.md; do
    if git -C "${repository_root}" check-ignore -q "${required_file}"; then
      fail "required repository file is not ignored: ${required_file}"
    else
      pass "required repository file is not ignored: ${required_file}"
    fi
  done
}

run_policy_tests() {
  test_repository_deployment_policy
}

group="${1:-all}"
case "${group}" in
  all) run_config_tests; run_caddy_tests; run_candidate_tests; run_transaction_tests; run_policy_tests ;;
  config) run_config_tests ;;
  caddy) run_caddy_tests ;;
  candidate) run_candidate_tests ;;
  transaction) run_transaction_tests ;;
  policy) run_policy_tests ;;
  *) printf 'unknown test group: %s\n' "${group}" >&2; exit 2 ;;
esac

printf '%s passed, %s failed\n' "${PASS_COUNT}" "${FAIL_COUNT}"
if (( FAIL_COUNT > 0 )); then
  exit 1
fi
