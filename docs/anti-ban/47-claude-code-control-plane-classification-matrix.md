# 47 Claude Code Control-Plane Classification Matrix

## 1. Scope and deployment assumptions

This matrix is the required design gate before changing local Claude Code control-plane behavior for the multi-provider runtime.

Deployment assumptions:

- Local `3012` and `3017` are development/test Sub2API deployments. Do not stop/rebuild `3012`.
- `http://198.12.67.185:18080` is a production-side composite ingress running the current Sub2API main branch plus the current CC Gateway main branch. It is exposed to local development because the server-side account database cannot be migrated locally.
- The server-side `18080` stack already has real Claude subscription formal-pool accounts. Treat it as production-sensitive even when it is used by a local canary.
- Cloud-side CC Gateway already protects the Anthropic/formal-pool side. The missing layer is local Claude Code CLI takeover control-plane classification and its trust contract with server-side Sub2API + CC Gateway.
- Local `x-sub2api-*` native attestation, route hints, and audit markers are local-only trust material. They must not be ordinary upstream passthrough headers to `18080` or Anthropic.

Two-stage control-plane contract:

```text
Claude Code CLI local request
  -> loopback guard classifies method/path/host
  -> guard strips local credentials and builds safe intent or approved native auxiliary route
  -> local Sub2API validates local attestation / route contract / pool isolation
  -> server-side Sub2API + CC Gateway binds selected account, persona, egress bucket, cache and upload policy
  -> only matrix-approved upstream fetch/upload/synthetic behavior can touch real formal-pool accounts
```

"Upload" in this document never means raw local credential/body passthrough. It means either:

1. safe intent upload to Sub2API for central policy/audit/quarantine; or
2. matrix-approved sanitized upstream behavior performed with selected formal-pool identity by server-side Sub2API + CC Gateway.

## 2. Evidence inventory

| Evidence source | Coverage | Safe-use notes |
|---|---|---|
| `docs/anti-ban/30-claude-code-control-plane-classification-strategy.md` | B1/B2 control-plane families, safe-intent principles, direct CONNECT guard | Design source; no raw payload needed. |
| `docs/anti-ban/35-formal-pool-control-plane-upload-strategy.md` | Two-stage safe intent -> server decision -> cache/upstream/synthetic strategy | Design source for production target. |
| `docs/anti-ban/38-formal-pool-synthetic-telemetry-strategy.md` | Telemetry/eval raw-never-upload and synthetic telemetry gates | Design source; synthetic telemetry remains off by default. |
| `docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md` | custom base URL native capability gaps, ToolSearch/defer_loading, official-host control-plane behavior | Design source for preserving native parity. |
| `docs/anti-ban/46-zhumeng-agent-claude-code-native-takeover-plan.md` | local guard/native takeover architecture and safe capture rules | Design source for local-only attestation and no raw logs. |
| `docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md` | CP0-CP8 multi-provider native/bridge split | Design source for pool isolation and route trust. |
| `docs/anti-ban/captures/real-baseline/2026-06-14-sub2api-cc-gateway-joint-local-capture/safe-deliverable/README.md` | Safe joint local capture for Claude Code 2.1.175; observed `/v1/messages`, count_tokens, event logging | Safe deliverable only; use path counts/status summaries, not raw body. |
| `docs/anti-ban/captures/real-baseline/2026-06-14-sub2api-cc-gateway-joint-local-capture/safe-deliverable/joint_local_capture_summary.redacted.json` | Redacted machine-readable capture for Claude Code 2.1.175 | Safe deliverable; do not expand raw refs. |
| `tools/cli_control_plane_policy.py` and tests | Current local Python guard defaults | Code source; current matrix is incomplete and must be expanded. |
| `backend/internal/service/control_plane_policy.go` and path matrix tests | Current Go control-plane router defaults | Code source; currently only a small safe-GET subset is explicit. |
| `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts` | CC Gateway main route policy | Current `selectSharedPoolRoute` supports `POST /v1/messages?beta=true`, defers `POST /v1/messages/count_tokens?beta=true`, suppresses event logging legacy/v2, blocks other control-plane paths. |

Checkpoint 21 local binding status:

- Go `NewDefaultControlPlanePathPolicyMatrix()` and Python `load_default_policy()` now explicitly classify the doc45/doc47 launch-time control-plane families listed in this matrix instead of letting known paths fall through the unknown-path bucket.
- The binding remains conservative: bootstrap/hello, public MCP registry, model-list metadata, and reviewed Claude Code feature-flag endpoints are local stub/cache-only; policy limits, remote managed settings, settings sync, team memory, model capabilities, GrowthBook, OAuth account/org settings, and unknown drift remain quarantine/block with no stale cache and no upstream fetch.
- This is a local guard/Sub2API safety binding, not approval for server-side CC Gateway fetches or direct official egress.

Version evidence status:

| Claude Code version | Evidence status | Matrix consequence |
|---|---|---|
| 2.1.150 | Historical lab/control-plane design evidence exists in docs and tests; active path matrix still needs explicit fixture references per row. | Rows without explicit 2.1.150 fixture remain conservative. |
| 2.1.175 | Strongest safe deliverable evidence in `2026-06-14-sub2api-cc-gateway-joint-local-capture`. | Baseline for initial native/formal-pool canary. |
| 2.1.177 | Mentions/tests exist, but per-family control-plane evidence is not complete in this matrix yet. | Treat as candidate drift; unknown/new rows quarantine until fixture-backed. |

## 3. Required matrix columns

Every implementation row must preserve these fields in code comments, tests, or fixture metadata:

```text
family
method
path_template
host_bucket
capture_evidence_versions
capture_or_fixture_sources
sensitive_risk
launch_action_local_guard
launch_action_sub2api
server_side_cc_gateway_cooperation
future_production_action
upstream_identity
raw_request_body_policy
raw_response_policy
safe_intent_fields
cache_scope
cache_ttl_seconds
cache_partition_keys
stale_policy
schema_allowlist
synthetic_or_shadow_eligibility
fail_closed_conditions
test_fixtures
targeted_tests
live_canary_gate
formal_pool_impact
bridge_pool_impact
codex_gateway_impact
```

## 4. Complete per-family field ledger

This ledger is the authoritative matrix. The compact table below is only a readable summary. Each row below explicitly carries every required field so implementation cannot infer missing behavior.

### 4.1 Native messages

- family: `native_messages`
- method: `POST`
- path_template: `/v1/messages?beta=true`; `/v1/messages` with equivalent `anthropic-beta` header must be tested as an explicit variant, not silently assumed.
- host_bucket: `local_loopback_guard -> local_sub2api -> server_sub2api_ccgateway_18080`
- capture_evidence_versions: `2.1.150=historical_docs_and_tests/evidence_gap_for_current_runtime`; `2.1.175=safe_joint_capture_2026_06_14`; `2.1.177=candidate_drift/evidence_gap_per_family`
- capture_or_fixture_sources: `docs/anti-ban/45-claude-code-custom-base-url-capability-delta.md`; `docs/anti-ban/46-zhumeng-agent-claude-code-native-takeover-plan.md`; `docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md`; `docs/anti-ban/captures/real-baseline/2026-06-14-sub2api-cc-gateway-joint-local-capture/safe-deliverable/*`; native guard/CCGateway tests.
- sensitive_risk: `P0_prompt_body_tools_cch`
- launch_action_local_guard: require native guard attestation; preserve native body shape; no official-host direct egress.
- launch_action_sub2api: admit only native catalog route; strip local internal headers before server-side gateway contract.
- server_side_cc_gateway_cooperation: current CC Gateway main forwards `POST /v1/messages?beta=true`; requires `x-cc-gateway-token`, `x-cc-provider`, `x-cc-account-id`, `x-cc-token-type`, `x-cc-policy-version`, `x-cc-egress-bucket`; binds selected account identity, persona/profile, session, egress bucket; signs/verifies CCH according to policy; fail closed on missing account, disabled bucket, persona mismatch, CCH verifier failure, or unsupported query.
- future_production_action: selected formal-pool account through CC Gateway sign-primary with session budget.
- upstream_identity: `selected_pool_account`
- raw_request_body_policy: transient upstream processing allowed for messages only; no logs, fixtures, safe reports, or persistent raw body.
- raw_response_policy: stream response to client; no raw persistence; safe status/model/tool-count summaries only.
- safe_intent_fields: route template, model name only if catalog-known, body key names, body size bucket, tools count bucket, thinking/context flags, header names, status bucket, scoped request refs.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`; session budget keyed by selected account + session opaque ref.
- stale_policy: `no_stale`
- schema_allowlist: native Anthropic messages schema including ToolSearch/tool_reference/defer_loading/thinking/context_management.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: missing/stale/forged native attestation; bridge route hint; body model/route mismatch; direct CONNECT; missing `x-cc-*`; unsupported query; persona/CCH verifier failure; local marker leak.
- test_fixtures: native messages golden; ToolSearch/defer_loading; local marker strip; CCGateway outbound header; route mismatch negative.
- targeted_tests: native guard tests; Go CCGateway boundary/admission tests; netwatch direct egress test.
- live_canary_gate: allowed only after localhost/mock native shape and CCGateway boundary tests pass; remote `18080` canary must be minimal and user-approved.
- formal_pool_impact: allowed native formal-pool path only.
- bridge_pool_impact: bridge models denied.
- codex_gateway_impact: no Codex Gateway group/account reuse.

### 4.2 Native count tokens / startup probe

- family: `native_count_tokens`
- method: `POST`
- path_template: `/v1/messages/count_tokens`; `/v1/messages/count_tokens?beta=true`
- host_bucket: `local_loopback_guard`; `server_sub2api_ccgateway_18080_only_if_approved`
- capture_evidence_versions: `2.1.150=historical_docs/evidence_gap`; `2.1.175=safe_joint_capture_route_family`; `2.1.177=candidate_drift/evidence_gap`
- capture_or_fixture_sources: docs 30/35/46; 2.1.175 safe deliverable; CC Gateway `selectSharedPoolRoute`.
- sensitive_risk: `P0_prompt_like_token_probe`
- launch_action_local_guard: local safe stub by default; no prompt persistence.
- launch_action_sub2api: native-only auxiliary route or local stub; bridge denied.
- server_side_cc_gateway_cooperation: current CC Gateway main returns `count_tokens_deferred` for `POST /v1/messages/count_tokens?beta=true` and blocks other variants; future support requires selected account/persona/egress proof and response schema verifier.
- future_production_action: selected account auxiliary count route only after server-side route support and tests.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no raw log/hash; transient only after explicit approval.
- raw_response_policy: launch stub summary only; future response schema allowlist before returning.
- safe_intent_fields: route template, body length bucket, body key names, model catalog class, startup_probe flag, status/action.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: launch stub schema only; future Anthropic count_tokens schema.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: prompt persistence attempt; bridge route; unsupported query; missing native attestation; CC Gateway `count_tokens_deferred` not handled.
- test_fixtures: startup count_tokens stub/deferred; no prompt leak; bridge denied.
- targeted_tests: Python guard count_tokens; Go route admission; CC Gateway deferred regression.
- live_canary_gate: no live `18080` count_tokens until server route support is explicitly approved.
- formal_pool_impact: none at launch except stub; future native auxiliary only.
- bridge_pool_impact: bridge denied.
- codex_gateway_impact: none.

### 4.3 Telemetry event logging v2

- family: `telemetry_event_logging_v2`
- method: `POST`
- path_template: `/api/event_logging/v2/batch`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=safe_joint_capture_route_family`; `2.1.177=evidence_gap/candidate_drift`
- capture_or_fixture_sources: docs 30/38/45; 2.1.175 safe deliverable; CC Gateway `selectSharedPoolRoute`; CC Gateway suppress tests.
- sensitive_risk: `P0_raw_telemetry_privacy_identity`
- launch_action_local_guard: discard raw after safe schema summary; return 204; emit telemetry safe intent.
- launch_action_sub2api: accept strict safe intent only; record scoped counters/quarantine, never messages route.
- server_side_cc_gateway_cooperation: current CC Gateway main suppresses route after gateway auth/persona checks; no upstream raw upload; future synthetic adapter must bind selected account, persona, session, egress policy, schema registry, and kill switch.
- future_production_action: synthetic telemetry only after gates.
- upstream_identity: `none_at_launch`; future synthetic uses `selected_pool_account`.
- raw_request_body_policy: raw never leaves guard; no raw digest/hash.
- raw_response_policy: local 204 only; no upstream response.
- safe_intent_fields: route template, header names, auth presence shape, body length bucket, registry-known top-level keys, known event enum only, unknown counts, suppress reason.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: scoped counters may use env + account opaque ref + session opaque ref + day + route template; no raw body material.
- stale_policy: `no_stale`
- schema_allowlist: telemetry intent schema; registry-known key names only.
- synthetic_or_shadow_eligibility: `shadow_generate_not_live`; real single-event canary requires separate approval.
- fail_closed_conditions: unknown event/field raw name exposure; CCH/billing marker; prompt/path/token/email/account UUID; raw digest; upload attempt without gate.
- test_fixtures: v2 telemetry intent; unknown field; CCH marker forbidden; no raw digest.
- targeted_tests: Python guard/intents; Go control-plane intent/quarantine; CC Gateway suppress.
- live_canary_gate: localhost replay + sensitive scan + explicit single-event approval.
- formal_pool_impact: no raw formal-pool upload at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.4 Telemetry event logging legacy

- family: `telemetry_event_logging_legacy`
- method: `POST`
- path_template: `/api/event_logging/batch`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=safe_joint_capture_route_family`; `2.1.177=evidence_gap/candidate_drift`
- capture_or_fixture_sources: docs 30/38; 2.1.175 safe deliverable; CC Gateway `selectSharedPoolRoute`.
- sensitive_risk: `P0_raw_telemetry_privacy_identity`
- launch_action_local_guard: same as v2; suppress 204.
- launch_action_sub2api: same as v2.
- server_side_cc_gateway_cooperation: current CC Gateway main suppresses legacy event logging; future synthetic should prefer v2 unless legacy-specific approval exists.
- future_production_action: legacy compatibility suppress; synthetic target v2 by default.
- upstream_identity: `none_at_launch`
- raw_request_body_policy: raw never.
- raw_response_policy: local 204 only.
- safe_intent_fields: same as v2 plus `legacy_path=true`.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: scoped counters only as v2.
- stale_policy: `no_stale`
- schema_allowlist: telemetry intent schema.
- synthetic_or_shadow_eligibility: `shadow_generate_not_live`
- fail_closed_conditions: same as v2; unexpected query.
- test_fixtures: legacy telemetry suppress; unknown event negative.
- targeted_tests: Python/Go control-plane; CC Gateway suppress.
- live_canary_gate: separate approval if ever needed.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.5 Eval

- family: `eval`
- method: `POST`
- path_template: `/api/eval/*`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=family_docs/evidence_gap_for_exact_paths`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/38; guard tests.
- sensitive_risk: `P0_eval_payload_private_context`
- launch_action_local_guard: suppress 204 or quarantine; sanitize path template only.
- launch_action_sub2api: safe intent/quarantine only.
- server_side_cc_gateway_cooperation: current CC Gateway main blocks unsupported eval routes; no upstream action.
- future_production_action: default suppress; future upload requires separate design/model approval.
- upstream_identity: `none`
- raw_request_body_policy: raw never.
- raw_response_policy: no upstream response; local suppress/quarantine response only.
- safe_intent_fields: method, `/api/eval/*`, body length bucket, schema known/unknown counts, action.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: eval safe intent only.
- synthetic_or_shadow_eligibility: `none_by_default`
- fail_closed_conditions: dynamic path leak; raw body; unknown schema; upload attempt.
- test_fixtures: eval suppress; path sanitizer.
- targeted_tests: Python guard/policy; Go quarantine.
- live_canary_gate: no live canary without new design approval.
- formal_pool_impact: none.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.6 Bootstrap / hello

- family: `bootstrap_hello`
- method: `GET`
- path_template: `/api/claude_cli/bootstrap`; `/api/hello`; `/v1/oauth/hello`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=docs_and_current_tests/evidence_gap_for_full_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/35/46; `tools/cli_control_plane_policy.py`; guard tests.
- sensitive_risk: `P1_feature_flags_native_shape`
- launch_action_local_guard: stub safe JSON + safe intent; allowlisted query keys only.
- launch_action_sub2api: optional safe-intent audit/cache stub; no raw local auth.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support this route; future server support must perform selected account binding, persona/profile, egress bucket, response schema verification, risk-text scan, and no-stale on auth/risk errors.
- future_production_action: account-scoped cached GET via selected account after schema allowlist.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: launch stub only; future raw upstream response may be transiently schema-scanned and reduced to allowlisted response, not persisted raw.
- safe_intent_fields: route template, query key names/enums, header names, auth presence shape, stub version, status.
- cache_scope: `session_or_user_when_enabled`
- cache_ttl_seconds: `300_max_initial`; launch stub may be deterministic without upstream cache.
- cache_partition_keys: env + account_ref + user/session opaque ref + persona_version + beta_profile + path_template + schema_version + key_version.
- stale_policy: `stale_safe_only_after_schema_approval`; no stale on 401/403/429/risk/schema drift.
- schema_allowlist: `ok`, `data`, `features` only until expanded by fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: unknown/repeated query; private response keys; schema drift; risk text; missing partition.
- test_fixtures: bootstrap/hello query variants; response scanner.
- targeted_tests: Python policy/guard; Go path matrix/cache.
- live_canary_gate: localhost/mock schema replay + explicit approval before `18080` upstream fetch.
- formal_pool_impact: future selected account only.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.7 Claude Code feature flags

- family: `claude_code_feature_flags`
- method: `GET`
- path_template: `/api/claude_code_penguin_mode`; `/api/claude_code_feature_flags`; `/api/claude_code_grove`; reviewed explicit `/api/claude_code_*`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs/fragments`; `2.1.175=current_tests/evidence_gap_for_full_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/35/45; extracted fragments; Python tests.
- sensitive_risk: `P1_feature_gate_native_parity`
- launch_action_local_guard: explicit path stub or block; wildcard quarantine.
- launch_action_sub2api: explicit rows only; no wildcard upstream.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future requires selected account/persona, feature schema verifier, private-field scan, and kill switch by path/profile.
- future_production_action: account-scoped cached fetch per explicit schema.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: launch stub; future allowlisted response only.
- safe_intent_fields: explicit path template, query key names, header names, version/profile, action.
- cache_scope: `user_session_persona`
- cache_ttl_seconds: `300_max_initial`
- cache_partition_keys: account_ref + user/session opaque ref + persona_version + beta_profile + path_template + schema_version + env + key_version.
- stale_policy: no stale until each path declares `stale_safe`.
- schema_allowlist: per-path keys only; wildcard forbidden.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: wildcard path; unknown query; private field; feature schema drift; stale wrong persona.
- test_fixtures: penguin/feature_flags/grove; wildcard negative.
- targeted_tests: Python policy/guard; Go matrix.
- live_canary_gate: per-path localhost replay + approval.
- formal_pool_impact: future selected account safe GET only.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.8 OAuth account settings

- family: `oauth_account_settings`
- method: `GET`
- path_template: `/api/oauth/account/settings`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=Go_policy_block/evidence_gap_for_safe_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/35; Go `control_plane_policy.go`.
- sensitive_risk: `P0_account_user_private_fields`
- launch_action_local_guard: quarantine/block.
- launch_action_sub2api: block unless strict fixture enables schema path.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future must bind selected account, scan response for account/org/user/email/token/private fields, and fail closed on auth/risk/schema drift.
- future_production_action: account-scoped cached fetch only after private-field proof.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: future response never persisted raw; only schema-allowlisted projection may return/cache.
- safe_intent_fields: route template, header names, auth presence shape, action/quarantine reason.
- cache_scope: `none_at_launch`; future `user_session`
- cache_ttl_seconds: `0_at_launch`; future `60_max_initial`
- cache_partition_keys: future account_ref + user/session opaque ref + path_template + schema_version + env + key_version.
- stale_policy: `no_stale` unless separately approved.
- schema_allowlist: launch none; future explicit keys only.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: private field; 401/403/429/risk; schema drift; missing partition.
- test_fixtures: blocked settings; private-field scanner; future allowlist fixture.
- targeted_tests: Go control-plane policy/cache/quarantine; Python guard.
- live_canary_gate: separate approval after schema replay.
- formal_pool_impact: selected account future only.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.9 OAuth org/referral

- family: `oauth_org_referral`
- method: `GET`
- path_template: `/api/oauth/organizations/{org}/...`; referral/eligibility variants by exact path only.
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=docs_35_design`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 35; future capture index.
- sensitive_risk: `P0_org_private_ids_referral_state`
- launch_action_local_guard: block/quarantine.
- launch_action_sub2api: block until selected-account org ID rebuild design exists.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future must rebuild org/account IDs from selected account metadata, not from client raw path, and scan response.
- future_production_action: high-risk account-scoped fetch with separate approval.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: no raw persistence; projection only if enabled.
- safe_intent_fields: path template only, no raw org/referral id; action/quarantine reason.
- cache_scope: `none_at_launch`; future `account_or_user_session`
- cache_ttl_seconds: `0_at_launch`; future `60_max_initial`
- cache_partition_keys: future selected account + rebuilt org opaque ref + session + schema_version + env.
- stale_policy: `no_stale`
- schema_allowlist: none until exact fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: client-supplied org id use; UUID/email leak; schema drift; missing selected account metadata.
- test_fixtures: org path redaction; exact path evidence required.
- targeted_tests: Python/Go unknown/quarantine.
- live_canary_gate: separate design approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.10 Claude Code organizations

- family: `claude_code_organizations`
- method: `GET`
- path_template: `/api/claude_code/organizations/{org}` and metrics variants by exact path.
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_fragments`; `2.1.175=current_tests/evidence_gap_for_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: current Python tests; docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md.
- sensitive_risk: `P1_or_P0_org_metrics`
- launch_action_local_guard: block/stub safe intent; do not trust raw org ID.
- launch_action_sub2api: block/stub.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future same selected-account org rebuild and response scan as OAuth org.
- future_production_action: selected-account fetch only after metadata rebuild.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: stub/projection only.
- safe_intent_fields: path template, org id omitted reason, action.
- cache_scope: `none_at_launch`; future `account_session`
- cache_ttl_seconds: `0_at_launch`; future `60_max_initial`
- cache_partition_keys: future account_ref + rebuilt org opaque ref + session + schema_version.
- stale_policy: `no_stale`
- schema_allowlist: none until fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: raw org ID in intent/log; unknown metrics path; private fields.
- test_fixtures: organizations redaction; metrics explicit path.
- targeted_tests: Python guard; Go quarantine.
- live_canary_gate: separate approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.11 MCP registry public

- family: `mcp_registry_public`
- method: `GET`
- path_template: `/mcp-registry/v0/servers`; reviewed public `/mcp-registry/*`
- host_bucket: `public_registry_or_official_control_plane_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=docs/tests/evidence_gap_for_full_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/35/38; Python policy/tests.
- sensitive_risk: `P2_public_but_tool_affecting`
- launch_action_local_guard: public stub/cache; no auth forwarding.
- launch_action_sub2api: public cache/stub with response scanner.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support and may not be needed; if server fetch is used, it should be public egress without selected formal-pool identity and with response schema verifier.
- future_production_action: public cached fetch.
- upstream_identity: `public_egress_or_none`
- raw_request_body_policy: no body.
- raw_response_policy: public response may be schema-scanned and cached after allowlist; no private fields.
- safe_intent_fields: path template, query key names, auth header presence false/stripped, status.
- cache_scope: `public`
- cache_ttl_seconds: `3600_max_after_review`; launch stub can be deterministic.
- cache_partition_keys: env + path_template + schema_version + key_version.
- stale_policy: stale-safe only for schema-allowlisted 2xx response.
- schema_allowlist: `data`, `servers` and reviewed public fields only.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: auth header present upstream; private credential fields; unknown registry path.
- test_fixtures: public registry empty/cache; no-auth forwarding.
- targeted_tests: Python policy/guard; Go cache/scanner.
- live_canary_gate: localhost/public mock first; real public fetch approval if not through formal pool.
- formal_pool_impact: no selected account.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.12 MCP private/account servers

- family: `mcp_servers_private`
- method: `GET`
- path_template: `/v1/mcp_servers`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=historical_docs`; `2.1.175=docs/current_go_stub/evidence_gap_for_private_response`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/35/38; Go `control_plane_policy.go`; Python tests.
- sensitive_risk: `P1_or_P0_private_server_credentials`
- launch_action_local_guard: empty stub + safe intent.
- launch_action_sub2api: user/session isolated cache/stub only.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future selected-account fetch must scan for server credentials and partition by user/session.
- future_production_action: account/user/session cached fetch after schema allowlist.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body.
- raw_response_policy: no raw persistence; projection only.
- safe_intent_fields: route template, limit query if allowlisted, header names, action.
- cache_scope: `user_session_when_enabled`
- cache_ttl_seconds: `60_max_initial`; launch stub no upstream.
- cache_partition_keys: account_ref + user/session opaque ref + path_template + schema_version + env + key_version.
- stale_policy: no stale until approved.
- schema_allowlist: `data`, `servers` with credential denylist.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: private credential fields; missing partition; schema drift.
- test_fixtures: empty stub; credential-deny negative.
- targeted_tests: Python/Go policy/cache/quarantine.
- live_canary_gate: separate approval after schema replay.
- formal_pool_impact: future selected account only.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.13 Web/domain info

- family: `web_domain_info`
- method: `GET`
- path_template: `/api/web/domain_info`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=fragment_mention`; `2.1.175=evidence_gap_exact_query`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: extracted fragments; future exact fixture.
- sensitive_risk: `P1_raw_domain_context`
- launch_action_local_guard: stub/block safe intent; redact query values.
- launch_action_sub2api: block/stub until query/schema reviewed.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future public/account fetch must enforce query allowlist and response scan.
- future_production_action: possible public/account fetch after review.
- upstream_identity: `none_at_launch`; future `public_egress_or_selected_pool_account_by_row`
- raw_request_body_policy: no body.
- raw_response_policy: stub/projection only.
- safe_intent_fields: path template, query key names only, query value omitted reason, action.
- cache_scope: `none_at_launch`
- cache_ttl_seconds: `0_at_launch`
- cache_partition_keys: TBD by approved row; default n/a.
- stale_policy: `no_stale`
- schema_allowlist: none until exact fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: raw domain/query value in logs/intent; unknown query.
- test_fixtures: domain query redaction.
- targeted_tests: Python guard; Go quarantine.
- live_canary_gate: exact fixture + approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.14 Policy limits

- family: `policy_limits`
- method: `GET` unless capture proves otherwise.
- path_template: `/api/claude_code/policy_limits`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=docs_30_45_mentions`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/45; future exact capture.
- sensitive_risk: `P1_native_limits_shape`
- launch_action_local_guard: explicit quarantine/block row; no upstream fetch.
- launch_action_sub2api: block/quarantine.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future selected account/persona fetch with response schema/risk verifier.
- future_production_action: selected account cached GET after proof.
- upstream_identity: `none_at_launch`; future `selected_pool_account`
- raw_request_body_policy: no body by default.
- raw_response_policy: projection only after schema allowlist.
- safe_intent_fields: exact path template, header names, action/quarantine reason.
- cache_scope: `none_at_launch`; future `account_session_persona`
- cache_ttl_seconds: `0_at_launch`; future `300_max_initial`
- cache_partition_keys: future account_ref + session opaque ref + persona_version + beta_profile + schema_version.
- stale_policy: no stale until approved.
- schema_allowlist: none until exact fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: unlisted path; private fields; stale wrong persona.
- test_fixtures: exact path quarantine; unknown drift negative.
- targeted_tests: Go/Python default policy matrix tests.
- live_canary_gate: exact fixture + approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.15 Remote managed settings / settings sync

- family: `remote_managed_settings_settings_sync`
- method: `GET` for local launch binding; `POST` remains unknown/quarantine until exact capture/design.
- path_template: `/api/claude_code/remote_managed_settings`; `/api/claude_code/settings_sync`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=docs_30_45_mentions`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/45; future exact capture.
- sensitive_risk: `P0_or_P1_user_team_private_state`
- launch_action_local_guard: explicit quarantine/block rows for the known service families; no raw settings upload/download.
- launch_action_sub2api: block/quarantine.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future requires separate design, selected account/user partition, response/private-field verifier, and no messages CCH reuse.
- future_production_action: separate design due private state.
- upstream_identity: `none_at_launch`
- raw_request_body_policy: raw private state never.
- raw_response_policy: no raw persistence; no projection until design.
- safe_intent_fields: path template only, method, body length bucket if POST, action/quarantine reason.
- cache_scope: `none_at_launch`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: none.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: any unlisted path; private field; upload attempt.
- test_fixtures: exact path quarantine; unknown drift; private-state negative.
- targeted_tests: Go/Python default policy matrix tests plus future POST capture tests before enabling.
- live_canary_gate: separate design approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.16 Team memory sync

- family: `team_memory_sync`
- method: `GET` for local launch binding; `POST` remains unknown/quarantine until exact capture/design.
- path_template: `/api/claude_code/team_memory`
- host_bucket: `official_control_plane_host_via_guard`
- capture_evidence_versions: `2.1.150=docs_30_45_mentions`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 30/45; future exact capture.
- sensitive_risk: `P0_private_memory`
- launch_action_local_guard: explicit quarantine/block row; no raw team memory upload/download.
- launch_action_sub2api: block/quarantine.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future requires separate design and selected account/user/session isolation.
- future_production_action: separate approval only.
- upstream_identity: `none_at_launch`
- raw_request_body_policy: raw never.
- raw_response_policy: no raw persistence.
- safe_intent_fields: path template, method, body length bucket if any, action.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: none.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: upload attempt; private field; unlisted path.
- test_fixtures: exact path quarantine; team-memory unknown drift.
- targeted_tests: Go/Python default policy matrix tests.
- live_canary_gate: separate approval.
- formal_pool_impact: none.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.17 Model capabilities / metadata

- family: `model_capabilities_metadata`
- method: `GET`
- path_template: `/v1/models`; `/api/claude_code/model_capabilities`
- host_bucket: `official_control_plane_host_via_guard_or_server_registry`
- capture_evidence_versions: `2.1.150=docs_45_mentions`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 45; persona/model registry code; future exact capture.
- sensitive_risk: `P1_native_capability_parity`
- launch_action_local_guard: `/v1/models` is local stub/cache-only; `/api/claude_code/model_capabilities` is explicit quarantine/block until exact schema evidence.
- launch_action_sub2api: local stub/block only; future server-maintained registry or selected-account fetch requires separate proof.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future selected account/persona or server registry must verify model/beta capabilities and fail closed on unknown major drift.
- future_production_action: selected account cached GET or server persona registry.
- upstream_identity: `none_at_launch`; future `selected_pool_account_or_server_registry`
- raw_request_body_policy: no body.
- raw_response_policy: projection only; local `/v1/models` stub may expose only empty `data`/`models` arrays.
- safe_intent_fields: path template, model id if catalog-known, persona version, action.
- cache_scope: `/v1/models=session`; blocked capability path has `none_at_launch`; future `account_persona_model`
- cache_ttl_seconds: `/v1/models=300`; blocked capability path `0_at_launch`; future `300_max_initial`
- cache_partition_keys: future account_ref + persona_version + model_id + beta_profile + schema_version.
- stale_policy: no stale for unknown model/persona drift.
- schema_allowlist: none until fixture.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: unknown model/path; stale wrong persona; schema drift.
- test_fixtures: `/v1/models` safe stub; exact capability fixture before enabling; unknown model negative.
- targeted_tests: Go/Python default policy matrix tests; persona/model registry and guard tests before future enablement.
- live_canary_gate: exact fixture + approval.
- formal_pool_impact: affects native catalog only after proof.
- bridge_pool_impact: separate bridge catalog; no native pollution.
- codex_gateway_impact: none.

### 4.18 GrowthBook / feature gates / analytics config

- family: `growthbook_feature_gates_analytics_config`
- method: `GET` for local launch binding; `POST` remains unknown/quarantine until exact capture/design.
- path_template: `/api/claude_code/growthbook`; reviewed feature flag stubs remain `/api/claude_code_penguin_mode`, `/api/claude_code_feature_flags`, `/api/claude_code_grove`, `/api/claude_code/organizations/metrics_enabled`
- host_bucket: `official_control_plane_or_analytics_host_via_guard`
- capture_evidence_versions: `2.1.150=docs_45_mentions`; `2.1.175=evidence_gap_exact_path`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 45; future exact capture.
- sensitive_risk: `P1_or_P0_identity_analytics`
- launch_action_local_guard: feature-flag endpoints are explicit local stub/cache-only rows; GrowthBook analytics/config endpoint is explicit quarantine/block.
- launch_action_sub2api: block/quarantine.
- server_side_cc_gateway_cooperation: current CC Gateway main does not support; future must use account/persona cache or local config with schema verifier and no raw analytics identity passthrough.
- future_production_action: account/persona cached fetch or local config after proof.
- upstream_identity: `none_at_launch`; future `selected_pool_account_or_public_egress_by_row`
- raw_request_body_policy: raw local analytics never.
- raw_response_policy: projection only.
- safe_intent_fields: host bucket, path template, method, header names, action.
- cache_scope: `none_at_launch`; future `persona_env`
- cache_ttl_seconds: `0_at_launch`; future `300_max_initial`
- cache_partition_keys: future persona_version + env + path_template + schema_version.
- stale_policy: no stale until explicit stale-safe.
- schema_allowlist: none until exact fixture.
- synthetic_or_shadow_eligibility: `shadow_only_after_design`
- fail_closed_conditions: unlisted host/path; private IDs; schema drift.
- test_fixtures: feature-flag stubs; GrowthBook exact path quarantine; exact host fixture required before enabling.
- targeted_tests: Go/Python default policy matrix tests; netwatch/unknown host tests.
- live_canary_gate: separate approval.
- formal_pool_impact: none at launch.
- bridge_pool_impact: none.
- codex_gateway_impact: none.

### 4.19 Official-domain CONNECT / direct bypass

- family: `official_domain_connect_direct_bypass`
- method: `CONNECT`
- path_template: `api.anthropic.com:443`; Claude hosts; MCP proxy hosts; non-Anthropic telemetry hosts by exact target.
- host_bucket: `process_network_egress`
- capture_evidence_versions: `2.1.150=historical_netwatch_docs`; `2.1.175=netwatch/safe_capture_family`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: docs 45/46; netwatch tests.
- sensitive_risk: `P0_bypass_local_server_policy`
- launch_action_local_guard: CONNECT stub only where needed; never create raw tunnel for messages; block unknown targets.
- launch_action_sub2api: quarantine bypass summaries only.
- server_side_cc_gateway_cooperation: all real egress must go through local/server Sub2API + CC Gateway; direct CONNECT is not server-side trust material.
- future_production_action: no direct official egress from local CLI.
- upstream_identity: `none_direct`
- raw_request_body_policy: raw never via CONNECT tunnel.
- raw_response_policy: no raw tunnel response except safe stub.
- safe_intent_fields: target host bucket, method CONNECT, action, bypass count.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: no tunnel schema; stub only.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: tunnel established for messages; direct `/v1/messages`; unknown target; raw TLS tunnel.
- test_fixtures: no-tunnel; official-host messages blocked; unknown target.
- targeted_tests: netwatch bypass; guard network safety.
- live_canary_gate: none.
- formal_pool_impact: protects formal pool.
- bridge_pool_impact: protects bridge isolation.
- codex_gateway_impact: protects Codex Gateway from accidental reuse.

### 4.20 Unknown drift

- family: `unknown_drift`
- method: `ANY`
- path_template: `unlisted_method_path_host_query`
- host_bucket: `any`
- capture_evidence_versions: `2.1.150=evidence_gap`; `2.1.175=evidence_gap`; `2.1.177=evidence_gap`
- capture_or_fixture_sources: fuzz tests; future safe capture summaries only.
- sensitive_risk: `P0_default_deny`
- launch_action_local_guard: quarantine/block + safe drift summary.
- launch_action_sub2api: quarantine/block.
- server_side_cc_gateway_cooperation: none until classified; server should reject unsupported route.
- future_production_action: review before allow/stub/fetch.
- upstream_identity: `none`
- raw_request_body_policy: raw never.
- raw_response_policy: local error only.
- safe_intent_fields: method, normalized path template if safely derived, host bucket, header names, body length bucket, quarantine reason.
- cache_scope: `none`
- cache_ttl_seconds: `0`
- cache_partition_keys: `n/a`
- stale_policy: `no_stale`
- schema_allowlist: none.
- synthetic_or_shadow_eligibility: `none`
- fail_closed_conditions: always until matrix row exists.
- test_fixtures: unknown path/method/query fuzz.
- targeted_tests: Python/Go unknown quarantine.
- live_canary_gate: no live canary.
- formal_pool_impact: no formal-pool reachability.
- bridge_pool_impact: no bridge reachability.
- codex_gateway_impact: no Codex Gateway reachability.

## 5. Classification matrix summary

### Legend

- `safe-intent`: only route template, method, host bucket, header names, auth presence shape, body size bucket, schema summary, status/action, and scoped refs. No raw values.
- `selected_pool_account`: server-side Sub2API + CC Gateway selected formal-pool account identity, persona, and egress bucket.
- `public_egress`: no formal-pool account identity; still schema scanned and cached.
- `not_live`: may exist as a design target, but disabled for launch until explicit gate passes.

### Matrix rows

| Family | Method / path template | Host bucket | Evidence | Sensitive risk | Launch action: local guard | Launch action: local Sub2API | Server-side CC Gateway cooperation | Future production action | Raw policy | Cache / TTL / partition | Synthetic/shadow | Fail-closed conditions | Fixtures / tests | Pool impact |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Native messages | `POST /v1/messages?beta=true`; `POST /v1/messages` with equivalent Anthropic beta header | local loopback -> local Sub2API -> `18080` composite | docs 45/46/47; 2.1.175 safe joint capture; native guard tests | P0: prompt/body/tools/CCH capable | forward only through guard; require native attestation | admit only native route/catalog; strip local internal headers before upstream contract | `selectSharedPoolRoute` forwards only `POST /v1/messages?beta=true`; requires `x-cc-gateway-token`, provider, account id, token type, policy version, egress bucket, persona/session gates | CC Gateway sign-primary under selected formal-pool account | request body transient upstream only; no logs/fixtures/raw persistence | no response cache; session budget ledger only | none | missing/stale/forged native attestation; bridge hint; route/body mismatch; direct CONNECT; missing `x-cc-*`; persona/CCH verifier fail | `test_native_guard_forwards_attested_native_markers_without_prompt_leak`; Go CCGateway/native admission tests; ToolSearch/defer_loading fixture | formal-pool only; bridge denied; Codex Gateway unaffected |
| Native count tokens / startup probe | `POST /v1/messages/count_tokens`; optional `?beta=true` | local loopback / `18080` if approved | docs 30/35/46; 2.1.175 safe capture shows route family; CC Gateway main currently returns `count_tokens_deferred` for `?beta=true` | P0: prompt-like body/token counting | local safe stub unless explicitly approved | native-only auxiliary route; no bridge admission | current CC Gateway main blocks/defer; no upstream count_tokens without future contract | selected account auxiliary route only after CC Gateway route support + tests | body may contain prompt; no raw log/hash; transient only if approved | no shared cache unless prompt-free summary; default none | none | any prompt persistence, bridge route, unsupported query, CC Gateway `count_tokens_deferred` not handled | startup probe fixture; no prompt leak test; CC Gateway count_tokens deferred regression | native only; bridge denied |
| Telemetry event logging v2 | `POST /api/event_logging/v2/batch` | official/control-plane host via guard | docs 30/38/45; 2.1.175 safe capture; CC Gateway suppress route exists | P0: may contain local paths, prompts, tool args, env, IDs | discard raw after schema summary; emit safe-intent; return 204 | accept safe-intent only; do not call messages route | current CC Gateway main suppresses v2 only after gateway auth/persona checks; no raw upstream | synthetic telemetry only after schema registry + localhost replay + separate real-pool approval | raw never leaves guard; no raw digest/hash | no response cache; scoped counters only, partitioned by env/account/session/day if needed | shadow/synthetic `not_live` | unknown event name/field, CCH/billing marker, raw digest attempt, prompt/path/token/email/UUID detected | telemetry intent fixture; scanner; unknown-field quarantine; CC Gateway suppress test | no bridge/formal-pool pollution; future selected account only for synthetic |
| Telemetry event logging legacy | `POST /api/event_logging/batch` | official/control-plane host via guard | docs 30/38; CC Gateway suppress route exists | P0 | same as v2 | same as v2 | current CC Gateway main suppresses legacy only | legacy compatibility suppress; synthetic target should prefer v2 | raw never | no cache | shadow/synthetic `not_live` | same as v2; legacy unexpected query blocks | legacy telemetry fixture | no bridge/formal-pool pollution |
| Eval | `POST /api/eval/*` | official/control-plane host via guard | docs 30/38; capture family evidence | P0: arbitrary eval payload/private context | suppress 204 or quarantine; safe path template only | safe-intent/quarantine only | current CC Gateway main does not support eval; would block unsupported route | default suppress; any future upload needs separate design approval | raw never; no path dynamic values | none | none by default | dynamic path value leak; raw body; unknown schema; upload attempt | eval suppress fixture; path sanitizer test | no formal-pool upload |
| Bootstrap / hello | `GET /api/claude_cli/bootstrap`; `GET /api/hello`; `GET /v1/oauth/hello` | official/control-plane host via guard | docs 30/35/46; current Python tests | P1: feature flags/settings can change native shape | stub safe JSON + safe-intent; allowlisted query only | optional safe-intent + cache/stub; no raw local auth | current CC Gateway main does not support; server-side fetch requires new route/adapter | account-scoped cached GET via selected account after schema allowlist | no body; no local auth/cookie passthrough | session/user partition; TTL 300s default; stale-safe only for approved schema | none | unknown query, private response fields, 401/403/risk, schema drift, cache partition missing | bootstrap/hello fixtures; query fuzz; response scanner | formal-pool future safe GET only; bridge isolated |
| Claude Code feature flags | `GET /api/claude_code_penguin_mode`; `GET /api/claude_code_feature_flags`; `GET /api/claude_code_grove`; reviewed explicit `GET /api/claude_code_*` | official/control-plane host via guard | docs 30/35/45; extracted fragments/tests | P1: feature gates/tool behavior/native parity | explicit path rows stub or block; wildcard quarantine | explicit rows only; no wildcard live fetch | current CC Gateway main does not support | account-scoped cached fetch per explicit response schema | no body | user/session/persona/version/beta partition; short TTL; no stale by default until proven | none | wildcard path, unknown query, private fields, feature shape drift | one fixture per explicit path; wildcard negative | future formal-pool safe GET only |
| OAuth account settings | `GET /api/oauth/account/settings` | official/control-plane host via guard | docs 30/35; Go default currently blocks | P0/P1: account/user/org-private fields | quarantine/block in launch | block unless explicit fixture enables strict schema | current CC Gateway main does not support | account-scoped cached fetch only after private-field proof | no body; no raw account/user IDs | account/user/session partition; no stale unless separately approved | none | private field, 401/403/risk, schema drift, partition missing | blocked fixture; future allowlist fixture | selected formal-pool account only if enabled |
| OAuth org/referral | `/api/oauth/organizations/{org}/...`; referral/eligibility variants | official/control-plane host via guard | docs 35 mentions; active exact-path fixture needed | P0: org/referral/private IDs | block/quarantine | block until exact path and ID rebuild design exists | current CC Gateway main does not support | future only with org/account id rebuilt from selected pool account metadata | no body; no raw org id | account/session partition; no stale default | none | client-supplied org id, UUID/email leak, schema drift | org path redaction fixture; exact path capture needed | selected account only after approval |
| Claude Code organizations | `GET /api/claude_code/organizations/{org}` and metrics variants | official/control-plane host via guard | docs/anti-ban/47-zhumeng-agent-claude-code-multi-provider-runtime-patch-plan.md; current Python tests mention metrics | P1/P0 depending response | block/stub safe intent; do not trust client org id | block/stub | current CC Gateway main does not support | same as OAuth org after selected account metadata rebuild | no body; no raw org id | account/session partition; no stale default | none | client org id in safe intent/log; unknown metrics path | organizations fixture; redaction test | selected account only after approval |
| MCP registry public | `GET /mcp-registry/v0/servers`; reviewed public `/mcp-registry/*` | official/public registry host via guard | docs 30/35/38; current Python tests | P2/P1: public but can affect tools | public stub/cache; no auth forwarding | public cache/stub; response scan | current CC Gateway main does not support; may not need selected account | public cached fetch with allowlist after review | no body; no auth | public cache; TTL <= 3600s; no user/account data | none | auth header present, private fields, unknown registry path | public registry fixture; no-auth forwarding test | no formal-pool account required |
| MCP private/account servers | `GET /v1/mcp_servers` | official/control-plane host via guard | docs 30/35/38; Go default stubs | P1/P0: server credentials/user state | empty stub + safe-intent | user/session isolated cache/stub | current CC Gateway main does not support | selected account/user-session cached fetch after schema allowlist | no body | user/session partition; short TTL; no stale until approved | none | credential/private fields, partition missing, schema drift | empty stub fixture; credential-deny scanner | selected account only if enabled; bridge isolated |
| Web/domain info | `GET /api/web/domain_info` | official/control-plane host via guard | extracted fragment mention; exact active fixture needed | P1: raw domain/user browsing context | stub/block safe intent; redact query values | block/stub until query/schema reviewed | current CC Gateway main does not support | possible public/account fetch after query allowlist | no body; no raw domain in logs | TBD; default none | none | raw domain/query value leak; unknown query | domain redaction fixture needed | no formal-pool until approved |
| Policy limits | exact paths from capture/source | official/control-plane host via guard | docs 30/45 mention; exact path evidence gap | P1: model/tool limits/native behavior | block/quarantine until explicit row | block/quarantine | current CC Gateway main does not support | selected account cached GET after schema proof | no body | account/session/persona partition | none | unlisted path, private fields, schema drift | exact path fixture required | future selected account |
| Remote managed settings / settings sync | exact paths from capture/source | official/control-plane host via guard | docs 30/45 mention; evidence gap | P0/P1: user/team state | block/quarantine | block/quarantine | current CC Gateway main does not support | separate design due private state | raw private state never | user/session partition if enabled; no stale | none | any unlisted path or private field | unknown drift fixture | no launch upstream |
| Team memory sync | exact paths from capture/source | official/control-plane host via guard | docs 30/45 mention; evidence gap | P0: private memory | block/quarantine | block/quarantine | current CC Gateway main does not support | separate design/approval only | raw never | user/session partition if ever enabled; no stale | none | any upload attempt or private field | unknown drift fixture | no launch upstream |
| Model capabilities / metadata | exact paths from capture/source | official/control-plane host via guard | docs 45 mentions capability cache; exact path evidence gap | P1: native capability parity | block/stub until exact row | block/stub | current CC Gateway main does not support | selected account or server-maintained persona registry | no body | persona/version/model partition; TTL reviewed | none | unknown model/path, stale wrong persona | exact fixture required | affects native model catalog; bridge separate |
| GrowthBook / feature gates / analytics config | exact paths/hosts from capture/source | official/control-plane/analytics host via guard | docs 45 mentions GrowthBook endpoint behavior; exact path evidence gap | P1/P0 depending payload | block/quarantine unless explicit public-safe row | block/quarantine | current CC Gateway main does not support | account/persona cached fetch or local config, after schema proof | raw local analytics never | persona/version/env partition | shadow only after design | unlisted host/path, private IDs, schema drift | exact fixture required | native only after approval |
| Official-domain CONNECT / direct bypass | `CONNECT api.anthropic.com:443`; Claude hosts; MCP proxy; non-Anthropic telemetry egress | local process netwatch/CONNECT | docs 45/46; netwatch tests | P0: bypasses local/server policy | CONNECT stub where needed; never tunnel messages | block/quarantine direct messages or unknown target | all real egress must be via local/server Sub2API+CC Gateway | no direct official egress from local CLI | raw never via tunnel | none | none | CONNECT tunnel established, direct `/v1/messages`, unknown target | netwatch bypass, no-tunnel tests | protects all pools |
| Unknown drift | any unlisted method/path/host/query | any | all docs | P0 default | quarantine/block + safe drift summary | quarantine/block | none | review required before allow/stub/fetch | raw never | none | none | any unlisted route/method/query | fuzz unknown tests | no pool impact allowed |

## 6. Pool isolation matrix

| Pool namespace | Groups/accounts | Allowed client/route evidence | Model catalog visibility | Upstream binding | Forbidden behavior | Audit namespace |
|---|---|---|---|---|---|---|
| Claude Code native formal-pool | `zhumeng-claude-code-native` and canary clone groups only | trusted local guard native attestation + server catalog native model + non-stale route contract | Claude native models only | server-side Sub2API + CC Gateway formal-pool selected account | bridge model, Codex Gateway account, forged native header, ordinary passthrough, direct Anthropic | `claude_code_native_*` |
| Claude Code bridge pools | `zhumeng-claude-code-bridge-openai`, `...-deepseek`, `...-agnes`, `...-anthropic-compat`, future `...-glm`, `...-kimi` | bridge route hint + bridge catalog + live flag after route trust green | overlay proof only until route trust green; then bridge models | `claude_code_bridge_*` upstream pools only | using native group 8/formal-pool account, native attestation, CCH/native control-plane | `claude_code_bridge_*` |
| Codex Gateway pools | existing Codex Desktop/Codex Gateway groups | Codex Gateway auth/route only | Codex Desktop models | existing Codex Gateway upstreams | reuse by Claude Code native/bridge without explicit dedicated group | `codex_gateway_*` |

## 7. Server-side CC Gateway cooperation summary

Current CC Gateway main, from `src/policy.ts::selectSharedPoolRoute`, supports only:

- `POST /v1/messages?beta=true` -> forward messages;
- `POST /v1/messages/count_tokens?beta=true` -> fail closed with `count_tokens_deferred`;
- `POST /api/event_logging/batch` -> suppress locally;
- `POST /api/event_logging/v2/batch` -> suppress locally;
- other methods/routes -> block unsupported.

Therefore, launch must not assume server-side CC Gateway can fetch bootstrap, account settings, MCP registry, private MCP servers, org settings, policy limits, managed settings, model capabilities, GrowthBook, settings sync, team memory, eval, or arbitrary control-plane paths. Those rows remain local stub/block/cache-only until a separate CC Gateway/server-side Sub2API worktree implements and tests them.

Required server-side trust material for any supported formal-pool request:

```text
x-cc-gateway-token
x-cc-provider
x-cc-account-id or safe account ref
x-cc-token-type
x-cc-policy-version
x-cc-egress-bucket
trusted persona/context headers only where CC Gateway already validates them
```

Local `x-sub2api-*` headers are not server-side upstream trust material.

## 8. Safe intent field policy

Allowed by default:

- method;
- path template, not raw dynamic path segments;
- host bucket, not raw host credentials;
- header names only;
- auth presence shape, never values;
- body length bucket;
- schema summary using allowlisted key names only;
- known event type enum only if registry-approved;
- action/status;
- scoped opaque refs/HMACs when keyed by environment, tenant/session, path template, purpose, and rotation period.

Forbidden everywhere:

- raw prompt/messages/tool args/tool schemas;
- raw telemetry/eval body;
- raw local file paths, working directory, git info, environment values;
- Authorization, cookie, `x-api-key`, proxy credentials;
- CCH or billing material in control-plane;
- email, account UUID, org UUID, user UUID;
- plain SHA/MD5 or long-term deterministic digest of raw body;
- raw query values for sensitive/dynamic fields.

## 9. Cache and stale policy

Default cache policy:

| Scope | Allowed rows | Required partition keys | Default TTL | Stale fallback |
|---|---|---|---:|---|
| none | telemetry/eval/unknown/private sync | n/a | 0 | never |
| public | MCP registry/public metadata only | path_template + schema_version + env + key_version | <= 3600s after review | only if schema allowlist passed |
| account | bootstrap/feature flags/model capabilities if enabled | account_ref + persona_version + beta_profile + path_template + schema_version + env + key_version | <= 300s initial | no stale on 401/403/429/risk/schema drift |
| user-session | private MCP/account settings/org/settings if ever enabled | account_ref + user/session opaque ref + persona_version + path_template + schema_version + env + key_version | <= 60s initial | disabled unless separately approved |

Cache hits are correctness constraints. Any safe summary or safe tool result used across provider switches must be frozen once, stable-hashed with scoped HMAC, and reused idempotently inside TTL. Cross-account, cross-user, cross-provider, or cross-purpose cache reuse is forbidden.

## 10. Synthetic telemetry gates

Synthetic telemetry remains `not_live` for this launch.

Required gates before any real-pool telemetry upload:

1. schema registry built from safe fixtures only;
2. local safe intent accepted by strict schema;
3. synthetic payload generated only from safe messages lifecycle summary + selected account/persona/session;
4. sensitive scanner passes across queue, audit, cache, fixtures, and mock evidence;
5. localhost replay passes;
6. single-event real-pool canary receives explicit approval;
7. kill switch, per-account budget, backoff, and quarantine are active;
8. telemetry failure cannot affect messages routing.

Eval remains suppress/quarantine unless a separate approved design says otherwise.

## 11. Fixture and targeted test plan

| Row family | Required fixture/test names |
|---|---|
| native messages | native request shape with ToolSearch/defer_loading; local marker strip; CCGateway `x-cc-*` outbound; route mismatch negative |
| count_tokens | startup count_tokens stub/deferred fixture; no prompt leak; bridge denied |
| event logging | v2 and legacy suppress fixtures; unknown event quarantine; CCH marker forbidden; no raw digest |
| eval | eval suppress fixture; dynamic path redaction; raw upload negative |
| bootstrap/hello | query allowlist fixture; stub schema; unknown query quarantine |
| feature flags | penguin/feature_flags/grove fixtures; wildcard negative |
| account/org settings | blocked fixture; private-field scanner; future schema allowlist fixture |
| MCP registry | public empty/cache fixture; no auth forwarding |
| MCP servers | empty stub; private credential field negative |
| web/domain info | query redaction fixture; unknown query negative |
| policy/settings/team memory/model capabilities/GrowthBook | exact path evidence fixture before enable; unknown drift negative |
| CONNECT/direct bypass | no raw tunnel; official-host messages blocked; unknown target blocked; netwatch bypass count |
| pool isolation | native formal-pool vs bridge vs Codex Gateway group/account negative matrix |

## 12. Implementation order enforced by this matrix

1. Keep ordinary passthrough header boundary fix first: local `x-sub2api-*` never leaks upstream.
2. Commit this matrix before control-plane code changes.
3. Implement local guard and Go server policy rows conservatively: stub/suppress/block first.
4. Do not enable live bridge routing before route trust contract tests pass.
5. Do not add server-side CC Gateway/Sub2API behavior without separate worktree and approval.
6. Do not run any live control-plane upload/canary against `18080` unless the row is matrix-approved, localhost/mock replay passed, and the user explicitly approved the real-pool canary.
