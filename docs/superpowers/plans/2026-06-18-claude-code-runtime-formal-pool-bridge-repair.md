# Claude Code Runtime Formal-Pool / Bridge Repair Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the Zhumeng Claude Code Runtime trust boundary so Claude native formal-pool traffic reaches the remote Sub2API + CC Gateway safely, while GPT/DeepSeek/AGNES/GLM/Kimi bridge traffic remains in isolated Claude Code bridge pools and control-plane requests are classified safely.

**Architecture:** Claude Code CLI still talks only to the local loopback guard; the guard sends native/bridge evidence to local Sub2API only. Local `x-sub2api-*` native attestation, route hints, and audit markers are internal trust material and must never be ordinary upstream passthrough headers. Claude native formal-pool egress must use a proper CC Gateway or gateway-to-gateway trust contract; bridge providers use disabled placeholders until the routing trust contract is green and never enter the Claude formal pool.

**Tech Stack:** Go backend, Python guard/runtime tooling, Postgres canary metadata, Docker canary app on `3017`, CodeGraph, and TypeScript CC Gateway only if a separate CC Gateway worktree becomes necessary.

---

## Hard Safety Rules

- Work only in `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime` for Sub2API changes.
- Do not edit the main Sub2API worktree.
- Do not stop, rebuild, or restart existing `3012`.
- Treat `http://198.12.67.185:18080` as a production-side Sub2API main + CC Gateway main composite ingress that already has real Claude subscription formal-pool accounts. It is exposed for local development/testing because the server-side account DB cannot be migrated locally, but it is not a mock endpoint.
- Local Claude Code control-plane work must complement, not bypass, server-side CC Gateway protection: local guard/Sub2API classifies and sanitizes requests into safe intent or approved native/auxiliary routes; server-side Sub2API + CC Gateway remains responsible for selected-pool-account identity, persona, egress bucket, upstream upload, synthetic telemetry gates, and final formal-pool safety.
- Do not send prompts, raw bodies, raw telemetry/eval, raw CCH, local Authorization/cookies/API keys, or local `x-sub2api-*` trust headers to `http://198.12.67.185:18080` as ordinary passthrough. Any live control-plane upload/canary against this endpoint requires a matrix row, localhost/mock replay, and explicit approval if it can exercise real pool accounts.
- Treat the current Postgres database as shared by `3012` and `3017` until proven otherwise.
- Do **not** mutate existing production-visible records such as account `132` in place while the DB is shared.
- Canary DB changes must use disabled placeholders, cloned canary-only records, or 3017-only feature gates. Anything that can affect `3012` requires explicit user confirmation.
- Do not print or commit API keys, tokens, refresh tokens, credentials, cookies, raw prompts, raw bodies, raw telemetry, raw CCH, or raw provider responses.
- Do not read/print raw shared live config file lines. If config shape must be inspected, output only field presence or redacted lengths using a sensitive-key allow/deny list.
- Run tests/builds serially, not in parallel.
- Do not delete files/directories, run `git reset`, `git clean`, `git rebase`, `git checkout --`, `git restore`, `sudo`, `chmod -R`, or `chown -R` without user confirmation.
- If CC Gateway code must change, first create a CC Gateway worktree from `/Users/muqihang/chelingxi_workspace/cc-gateway` at `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-runtime-formal-pool-bridge-repair` on branch `codex/claude-code-runtime-formal-pool-bridge-repair`, then index it with CodeGraph. Do not edit CC Gateway main.
- If server-side Sub2API main needs changes beyond this local runtime worktree, do not edit or deploy the server-side main directly. Create a separate approved server-side worktree/branch plan first, with rollback and deployment approval.

---

## Current Facts to Preserve

1. `3017` currently runs a canary Sub2API binary but appears to use the same Postgres/data volume as `3012`.
2. DB has `zhumeng-claude-code-native` group, but no Claude Code bridge-specific groups yet.
3. Account `132` (`zhumeng-claude-code-native-upstream`) is currently an ordinary Anthropic API-key passthrough to `http://198.12.67.185:18080`. Do not edit it in place while DB is shared.
4. The `18080` endpoint is already the server-side production Sub2API + CC Gateway stack. The local gap is not cloud-side CC Gateway absence; the gap is local Claude Code control-plane classification and the trust contract between local Sub2API/guard and the server-side Sub2API + CC Gateway.
5. Local app-level `gateway.ccgateway.enabled/base_url/token/providers.anthropic` is not currently configured for 3017.
6. Current `shouldForwardClientHeaderToAnthropic` forwards local Claude Code native attestation/signature/internal headers; this conflicts with docs 46/47.
7. CC Gateway sub2api mode expects `x-cc-gateway-token`, selected account ref, provider, token type, policy version, egress bucket, and trusted persona/context headers. It should not trust local guard attestation headers.
8. Lab captures show control-plane families beyond `/v1/messages`: `count_tokens`, event logging, eval, bootstrap, penguin mode, MCP registry, organizations, and web domain info.

---

## File Map

### Sub2API backend

- `backend/internal/service/gateway_service.go`
  - Fix ordinary Anthropic passthrough header filtering.
  - Ensure local `x-sub2api-*` markers are stripped unless handled by an explicit internal path.

- `backend/internal/service/cc_gateway_adapter.go`
  - Confirm/tighten fail-closed behavior when formal-pool accounts require CC Gateway but app config is incomplete.
  - Confirm `x-cc-*` header construction is the only CC Gateway trust material sent upstream.

- `backend/internal/service/gateway_anthropic_apikey_passthrough_test.go`
  - Replace tests expecting local native headers to be forwarded.
  - Add generalized `x-sub2api-*` strip tests.

- `backend/internal/service/cc_gateway_adapter_test.go`
  - Add missing token/base URL/provider fail-closed tests.
  - Add CC Gateway outbound header tests.

- `backend/internal/service/gateway_cc_gateway_control_plane_test.go`
  - Add formal-pool no-silent-fallback tests.

- `backend/internal/service/claude_code_native_route_admission_test.go`
  - Ensure backend formal-pool admission uses server-side catalog/native attestation, not client route hint.

### Python guard/runtime

- `tools/cli_control_plane_policy.py`
  - Control-plane route matrix.

- `tools/cli_control_plane_guard.py`
  - Safe intent/stub/suppress/block behavior.
  - Native route adds local native attestation only to local Sub2API.
  - Bridge route never gets native attestation.

- `tools/claude_code_route_trust.py`
  - Native/bridge route contract and live-disabled bridge defaults.

- `tools/zhumeng-agent/src/zhumeng_agent/adapters/claude_code/guard.py`
  - Ensure managed launcher points Claude Code to loopback guard and guard to 3017.

### Canary config / DB setup

- Optional new script: `tools/claude_code_runtime_canary_config.py`
  - Must be idempotent and dry-run by default.
  - Must not print secrets.
  - Before route contract is green, it may create disabled placeholder groups only.
  - It must not edit account `132` in place.

### Docs

- Create/modify: `docs/anti-ban/47-claude-code-control-plane-classification-matrix.md`
  - Mandatory design gate before control-plane code changes. It records path family, capture evidence, sensitive-risk class, launch action, future production action, safe-intent/upload policy, cache/TTL/isolation, CC Gateway cooperation point, fail-closed conditions, fixtures, and tests.

- `docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md`
  - Record safe evidence, tests, and remaining risks.

### CC Gateway, only if required

- Create separate worktree before edits.
- Likely files if needed: `src/proxy.ts`, `src/policy.ts`, `src/rewriter.ts`, and corresponding tests.

---

## Control-Plane Classification Design Baseline

This baseline is part of the execution plan. Before any new control-plane code change, Task 5A must turn it into a committed matrix by comparing Claude Code CLI lab captures (2.1.150, 2.1.175, 2.1.177 where available), docs 30/35/38/45/46/47, the Python guard policy, the Go control-plane router, and CC Gateway main behavior.

The design intent is not "never upload anything" and not "pass everything". The intent is a two-stage contract:

```text
Local Claude Code CLI request
  -> local loopback guard classifies path/method/host
  -> local guard strips local credentials and emits safe intent or approved native/auxiliary route
  -> local Sub2API verifies local attestation and applies route contract
  -> server-side Sub2API + CC Gateway binds selected formal-pool account/persona/egress bucket
  -> only matrix-approved upstream fetch/upload/synthetic behavior can reach real formal-pool accounts
```

Required matrix columns:

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

Initial classification table:

| Family | Method / path template | Launch action | Future production target | Raw policy | Cache / isolation | CC Gateway cooperation | Fixture/test requirement |
|---|---|---|---|---|---|---|---|
| Native messages | `POST /v1/messages?beta=true`; `POST /v1/messages` with equivalent Anthropic beta header | Forward only through loopback guard -> local Sub2API -> server Sub2API/CC Gateway boundary; no direct official egress | CC Gateway sign-primary with selected formal-pool account | Messages body transient upstream only; never logged/persisted | No cache; session budget ledger | `x-cc-*`/selected account/persona/egress bucket; no local `x-sub2api-*` passthrough | native shape, ToolSearch/defer_loading preservation, no local marker leak |
| Native count tokens | `POST /v1/messages/count_tokens` with/without `beta=true` | Stub/local safe response unless explicit native auxiliary route is approved | Selected formal-pool account via CC Gateway auxiliary/control-plane path if proven | Prompt-like body transient only if approved; no logs/digests | No shared cache unless prompt-free summary | Separate from messages CCH; native-only admission | startup probe, no prompt leak, bridge denied |
| Telemetry event logging | `POST /api/event_logging/v2/batch`; legacy `/api/event_logging/batch` | Safe intent + local 204/suppress; discard raw body | Synthetic telemetry only after schema registry, localhost replay, and separate real-pool approval | Never raw upload; no raw digest/hash | No response cache; scoped safe counters only | CC Gateway synthetic adapter only when enabled by policy | telemetry intent fixture, scanner, unknown field quarantine |
| Eval | `POST /api/eval/*` | Suppress 204 or quarantine | Default suppress; future upload requires separate design approval | Never raw upload | None | None by default | eval suppress and path-template sanitizer |
| Bootstrap / hello | `GET /api/claude_cli/bootstrap`; `GET /api/hello`; `GET /v1/oauth/hello` | Stub safe JSON + safe intent | Account-scoped cached fetch after schema allowlist | No body; no local auth passthrough | session/user partition; short TTL; stale only if approved | selected account/persona if upstream fetch enabled | query variants, response private-field scanner |
| Feature flags / Claude Code flags | `GET /api/claude_code_penguin_mode`; `GET /api/claude_code_feature_flags`; `GET /api/claude_code_grove`; reviewed `GET /api/claude_code_*` | Explicit path rows stub or block; wildcard quarantine | Account-scoped cached fetch per explicit schema | No body | user/session + persona/version/beta partition | selected account/persona; no wildcard pass | per-path fixture plus wildcard negative |
| Organization / org metrics | `GET /api/claude_code/organizations/{org}` and metrics variants | Block/stub safe intent; do not trust client org id | Only if org/account id is rebuilt from selected pool account metadata | No body; no raw org id in logs/intent | account/session partition; no stale default | selected account metadata rebuild | org redaction fixture |
| OAuth account/settings/org/referral | `GET /api/oauth/account/settings`; `/api/oauth/organizations/{org}/...` | Block unless matrix enables strict fixture | Account-scoped cached fetch after schema/private-field gates | No body; no raw account/org/user IDs | account/user/session partition; no stale default | selected account only | blocked fixture and future allowlist fixture |
| MCP registry public | `GET /mcp-registry/v0/servers`; explicit public `/mcp-registry/*` | Public stub/cache; no auth forwarding | Public cached fetch with response allowlist | No body | public cache; reviewed TTL | public egress or no selected account | no-auth forwarding, private-field scanner |
| MCP servers account list | `GET /v1/mcp_servers` | Empty stub + safe intent | Account/user/session isolated cached fetch after schema allowlist | No body | user/session partition; short TTL | selected account if enabled | empty stub and credential-deny fixtures |
| Web/domain info | `GET /api/web/domain_info` | Stub/block safe intent until query/schema reviewed | Possible public/account fetch after review | No body; no raw queried domain in logs | TBD, default none | TBD by row | domain query redaction fixture |
| Policy limits / remote settings / model capabilities / GrowthBook | exact paths from capture/source only | Block/quarantine until explicit row exists | Separate row per path with schema allowlist | No body by default | partition by account/user/session/persona as needed | selected account/persona if enabled | exact-path fixtures; unknown path negative |
| Settings sync / team memory sync | explicit captured paths only | Block/quarantine | Separate design because user-private state | No raw private state | user/session partition if ever enabled; no stale default | selected account only after approval | unknown drift fixture |
| Direct CONNECT / official host bypass | `CONNECT api.anthropic.com:443`, Claude hosts, MCP proxy hosts, non-Anthropic telemetry hosts | CONNECT stub only where needed; never raw tunnel for messages | no direct official egress from local CLI | Never via tunnel | None | all real egress via local/server Sub2API + CC Gateway | netwatch bypass and no-tunnel tests |
| Unknown drift | any unlisted method/path/host/query | quarantine/block + safe drift summary | requires review before allow/stub/fetch | Never | None | none | fuzz unknown tests |

Control-plane implementation must preserve these invariants:

1. All control-plane requests become safe intent first unless they are a matrix-approved native auxiliary route.
2. "Upload" means safe intent to Sub2API or matrix-approved sanitized upstream behavior; it never means raw local credential/body passthrough.
3. Safe GET/public paths may later use selected formal-pool identity or public egress with schema allowlists and scoped cache.
4. High-risk POST telemetry/eval never raw-upload; synthetic/shadow is a later gated module.
5. Unknown drift is permanently fail-closed until classified.
6. Control-plane never reuses messages CCH signing, never falls back into messages, and never causes bridge traffic to enter native formal-pool.
7. Because `18080` has real pool accounts, live remote control-plane canaries require matrix row + localhost/mock replay + explicit approval.

## Task 0: Freeze Safe Evidence Before Edits

**Files:** none.

- [ ] **Step 0.1: Confirm Sub2API worktree**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
pwd
git branch --show-current
git status --short
test -d .codegraph && echo CODEGRAPH_OK
codegraph status .
```

Expected: branch is `codex/claude-code-multiprovider-runtime`; `.codegraph/` exists.

- [ ] **Step 0.2: Sync CodeGraph**

Run:

```bash
codegraph sync /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
```

Expected: index sync succeeds.

- [ ] **Step 0.3: Confirm containers without touching 3012**

Run:

```bash
docker ps --format 'table {{.Names}}\t{{.Ports}}\t{{.Status}}' | rg '3012|3017|postgres|redis|sub2api'
```

Expected: `3012` remains up; `3017` is the only canary app to restart later.

- [ ] **Step 0.4: Capture safe DB topology snapshot**

Run exactly this redacted query. It prints no credential values, no `extra` values, and no base URLs:

```bash
cat > /tmp/sub2api_safe_topology_snapshot.sql <<'SQL'
\pset format unaligned
\pset fieldsep '\t'
select 'GROUP' as kind,id,name,platform,status,is_exclusive::text,claude_code_only::text,codex_gateway_entitled::text,augment_gateway_entitled::text,
       case when models_list_config is null then '' else '<json_present>' end as models_list_config_present
from groups
where id in (2,3,6,7,8) or name like 'zhumeng-claude-code%'
order by id;

select 'ACCOUNT' as kind,a.id,a.name,a.platform,a.type,string_agg(ag.group_id::text,',' order by ag.group_id) as groups,a.status,a.schedulable::text,
       (select string_agg(k,',' order by k) from jsonb_object_keys(coalesce(a.extra,'{}'::jsonb)) as k) as extra_keys,
       (select string_agg(k,',' order by k) from jsonb_object_keys(coalesce(a.credentials,'{}'::jsonb)) as k) as credential_keys
from accounts a
left join account_groups ag on ag.account_id=a.id
where a.id=132 or a.name like 'zhumeng-claude-code%' or ag.group_id in (2,3,6,7,8)
group by a.id
order by a.id;
SQL
docker exec -i sub2api-codex-gateway-live-postgres psql -U sub2api -d sub2api -X < /tmp/sub2api_safe_topology_snapshot.sql
```

Expected:
- Native group exists.
- Bridge groups are absent or disabled.
- Account `132` remains ordinary passthrough before repair.
- No secret/base URL/API key values are printed.

---

## Task 1: Fix Ordinary Anthropic Passthrough Header Boundary

**Files:**
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/service/gateway_anthropic_apikey_passthrough_test.go`
- Modify: `backend/internal/service/cc_gateway_adapter_test.go`

- [ ] **Step 1.1: Write failing strip tests**

Add tests proving ordinary Anthropic passthrough strips local internal headers:

- `x-sub2api-native-attestation`
- `x-sub2api-native-signature`
- `x-sub2api-client-type`
- `x-sub2api-guard-attested`
- route hint headers
- forged bridge/native client-type headers
- representative unknown `x-sub2api-*` internal header

Expected ordinary Anthropic passthrough still forwards legitimate public Anthropic headers such as `anthropic-version`, `anthropic-beta`, `x-app`, and `x-stainless-*` where existing policy allows them.

- [ ] **Step 1.2: Run failing test**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'AnthropicAPIKeyPassthrough.*Strip|LocalClaudeCodeInternalHeaders' -count=1
```

Expected before implementation: FAIL because local internal headers currently leak.

- [ ] **Step 1.3: Implement minimal filter**

In `gateway_service.go`, ensure `shouldForwardClientHeaderToAnthropic` returns false for local internal `x-sub2api-*` headers unless a separate explicit path handles them. Do not strip legitimate Anthropic/Stainless public headers.

Implementation direction:

```go
func isSub2APILocalInternalHeader(lowerKey string) bool {
    return strings.HasPrefix(lowerKey, "x-sub2api-")
}
```

Then call this from the ordinary client-header forwarding predicate.

- [ ] **Step 1.4: Add CC Gateway outbound regression test**

Test `useCCGateway=true` path:

- local `x-sub2api-*` headers are absent from outbound request;
- `x-cc-gateway-token`, `x-cc-account-id`, `x-cc-provider`, `x-cc-token-type`, `x-cc-policy-version`, and `x-cc-egress-bucket` are present;
- target is CC Gateway base URL + `/v1/messages?beta=true`;
- missing mandatory token/base URL/provider fails closed.

- [ ] **Step 1.5: Run targeted tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'AnthropicAPIKeyPassthrough|CCGatewayAnthropicAPIKeyPassthrough|CCGatewayControlPlane' -count=1
```

Expected: PASS.

- [ ] **Step 1.6: Sync CodeGraph and commit**

Run:

```bash
codegraph sync /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
git add backend/internal/service/gateway_service.go backend/internal/service/gateway_anthropic_apikey_passthrough_test.go backend/internal/service/cc_gateway_adapter_test.go
git commit -m "fix: keep claude code markers inside local trust boundary"
```

Expected: commit succeeds in Sub2API worktree only.

---

## Task 2: Confirm Server-Side Composite Ingress Contract

**Files:** none unless docs are updated later.

- [ ] **Step 2.1: Confirm remote contract from code/config evidence first**

Assume `http://198.12.67.185:18080` is the production-side Sub2API main + CC Gateway main composite ingress with real formal-pool accounts. Do not use live `/v1/messages`, `/v1/messages/count_tokens`, or control-plane paths as discovery probes. Confirm the expected contract from repository code, safe deployment notes, and redacted config shape:

```text
local guard trust material: local-only `x-sub2api-*`
local Sub2API -> server Sub2API/CCGateway trust material: explicit gateway contract, not ordinary Anthropic passthrough
server Sub2API/CC Gateway -> Anthropic formal-pool: selected account/persona/egress bucket/CCH or control-plane policy
```

If an HTTP probe is unavoidable, use only no-auth/no-body health-style status/header-name checks with a short timeout. Do not send keys, prompts, request bodies, local attestation headers, or raw control-plane payloads.

- [ ] **Step 2.2: Inspect 3017 container config shape without raw config lines**

Do not print raw shared config files. Inspect only 3017 container env, redacting sensitive names:

```bash
python3 - <<'PY'
import json, re, subprocess
name='sub2api-canary-app-main-f3a9f235d-cc-runtime-current-3017'
secret=re.compile(r'(key|secret|token|password|auth|credential|cookie|dsn|api)', re.I)
interesting=re.compile(r'(ccgateway|cc_gateway|gateway|claude|anthropic|sub2api|url|host|port)', re.I)
inspect=json.loads(subprocess.check_output(['docker','inspect',name], text=True))[0]
for item in sorted(inspect['Config'].get('Env') or []):
    k,v=(item.split('=',1)+[''])[:2]
    if not interesting.search(k):
        continue
    if secret.search(k):
        print(f'{k}=<redacted:{len(v)}>')
    elif k.endswith('URL') or k.endswith('BASE_URL'):
        print(f'{k}=<url_present:{bool(v)};length:{len(v)}>')
    else:
        print(f'{k}=<present:{bool(v)};length:{len(v)}>')
PY
```

Expected: know whether app-level CCGateway config is present without exposing secrets.

- [ ] **Step 2.3: Choose local-to-server contract branch**

Decision rules now assume the remote is the production-side Sub2API + CC Gateway composite stack:

- If existing local Sub2API can be configured to talk to server-side Sub2API+CCGateway through an explicit gateway contract (`x-cc-*` or reviewed gateway-to-gateway proof), use Task 3A with canary-only local records, not account `132`.
- If local code currently only supports ordinary Anthropic API-key passthrough to `18080`, fix local code/tests first; ordinary passthrough must not be used for native formal-pool or control-plane trust material.
- If the server-side main code lacks an ingress contract needed by the local runtime, stop at Task 3B design gate; do not change/deploy server-side code until the user approves a CC Gateway/Sub2API worktree plan.
- If CC Gateway or server-side Sub2API code changes are needed, perform Task 2.4 before any edit.
- If DB isolation from 3012 is not proven: DB writes must be disabled placeholders or cloned canary-only records gated away from 3012.

- [ ] **Step 2.4: Create CC Gateway worktree only if code changes are needed**

Run only if Task 2.3 requires CC Gateway edits:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
git worktree add .worktrees/claude-code-runtime-formal-pool-bridge-repair -b codex/claude-code-runtime-formal-pool-bridge-repair
cd .worktrees/claude-code-runtime-formal-pool-bridge-repair
codegraph init . || codegraph index .
codegraph status .
git status --short
```

Expected: clean CC Gateway worktree, no edits in CC Gateway main.

---

## Task 3A: Wire Local Sub2API to Server-Side Sub2API + CC Gateway Boundary

Use only if Task 2 confirms an explicit local-to-server gateway contract is available or can be implemented locally without server-side deployment. Do not use ordinary Anthropic API-key passthrough as the native formal-pool trust boundary.

**Files:**
- Modify tests: `backend/internal/service/cc_gateway_adapter_test.go`
- Modify tests: `backend/internal/service/gateway_cc_gateway_control_plane_test.go`
- Possible modify: `backend/internal/service/cc_gateway_adapter.go`
- Possible add: `tools/claude_code_runtime_canary_config.py`

- [ ] **Step 3A.1: Add fail-closed tests**

Test that formal-pool Anthropic account with `cc_gateway_enabled=true` fails closed if any app-level requirement is missing:

- `Gateway.CCGateway.Enabled`
- `Gateway.CCGateway.Providers.Anthropic`
- `Gateway.CCGateway.BaseURL`
- mandatory `Gateway.CCGateway.Token`

Also test that `anthropic_passthrough=true` plus `cc_gateway_enabled=true` cannot silently fall back to ordinary passthrough.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'CCGateway.*FailClosed|Passthrough.*CCGateway|FormalPool' -count=1
```

Expected before any needed implementation: fail if current behavior silently falls back.

- [ ] **Step 3A.2: Tighten implementation if tests reveal fallback**

In `cc_gateway_adapter.go` / related path, preserve or tighten formal-pool fail-closed behavior. Formal-pool native accounts must never silently fall back to ordinary API-key passthrough when CC Gateway config is incomplete.

- [ ] **Step 3A.3: Configure 3017 app-level server-side gateway settings only in canary runtime**

Do not edit shared raw config in a way that affects `3012`. Prefer canary container environment or a 3017-only config overlay. The setting names may remain `ccgateway` in local code, but semantically they point to the server-side Sub2API + CC Gateway composite ingress and must use a reviewed explicit gateway contract, not ordinary Anthropic passthrough.

Required settings:

```yaml
gateway:
  ccgateway:
    enabled: true
    base_url: http://198.12.67.185:18080
    token: <redacted mandatory x-cc-gateway-token value>
    default_egress_bucket: <remote-approved bucket if required>
    providers:
      anthropic: true
```

Do not print token values.

- [ ] **Step 3A.4: Use canary-only cloned native account; do not edit account 132**

Because DB may be shared, do not update account `132`. Create a canary-only clone if DB mutation is needed:

```text
group:   zhumeng-claude-code-native-canary-3017
account: zhumeng-claude-code-native-upstream-canary-3017
```

Requirements:

- Clone may copy remote base URL/API key internally, but script must never print values.
- Clone must not bind to Codex Gateway groups `2`, `6`, or `7`.
- Clone must be gated by 3017-only managed device/API key or feature flag so 3012 cannot schedule it.
- `cc_gateway_canary_only=true` unless DB isolation is proven or user approves otherwise.
- Clone must remove or ignore `anthropic_passthrough` when `cc_gateway_enabled=true`.
- If clone cannot be isolated from 3012, stop and ask the user before DB changes.

Minimum clone extras:

```json
{
  "cc_gateway_enabled": true,
  "cc_gateway_canary_only": true,
  "cc_gateway_policy_version": "2.1.175",
  "cc_gateway_routes": "native_messages,native_count_tokens",
  "cc_gateway_egress_bucket_enabled": true,
  "cc_gateway_egress_bucket": "<remote-approved-bucket-or-account-ref>"
}
```

- [ ] **Step 3A.5: Run targeted tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'CCGatewayAnthropic|CCGatewayControlPlane|FormalPool|Native|Passthrough' -count=1
```

Expected: PASS.

- [ ] **Step 3A.6: Rebuild and restart only 3017**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o ../artifacts/bin/sub2api-current-linux-arm64 ./cmd/server
```

Restart only `sub2api-canary-app-main-f3a9f235d-cc-runtime-current-3017` with the new binary and canary-only config. After restart, verify `3012` is still up:

```bash
docker ps --format 'table {{.Names}}\t{{.Ports}}\t{{.Status}}' | rg '3012|3017'
```

- [ ] **Step 3A.7: Run minimal native smoke**

Use managed guard path and a minimal prompt. Capture only safe status/evidence:

- guard ready;
- count_tokens startup probe returns 200 local stub or allowed control-plane response;
- `/v1/messages` reaches remote CC Gateway without local attestation mismatch;
- local `x-sub2api-native-attestation` is not ordinary upstream material;
- no bridge group is used.

- [ ] **Step 3A.8: Commit**

Commit code/tests/scripts/docs only; never commit runtime secrets:

```bash
git add <changed files>
git commit -m "fix: route claude code native through cc gateway boundary"
```

---

## Task 3B: Stop-and-Design Gate for Missing Server-Side Ingress Contract

Use only if Task 2 shows that server-side Sub2API + CC Gateway main lacks the explicit ingress contract required by the local Claude Code runtime. This is not automatic implementation or deployment.

**Files:**
- Design doc only until user approves remote-side changes.

- [ ] **Step 3B.1: Write design note**

Document:

```text
local guard attestation -> local Sub2API only
gateway-to-gateway proof -> local Sub2API to remote Sub2API
remote formal-pool admission -> remote Sub2API/CC Gateway only
```

- [ ] **Step 3B.2: Specify remote worktree/deploy/rollback plan**

Before code, specify:

- remote repo/worktree path;
- branch name;
- secret provisioning method;
- deploy target;
- rollback plan;
- tests.

- [ ] **Step 3B.3: Stop for user approval**

Do not implement/deploy server-side ingress until user approves the remote-side plan. If not approved, report that local runtime can only use local stub/mock or already-supported server-side contracts for this rollout.

---

## Task 4: Prepare Bridge Groups as Disabled Placeholders Only

**Files:**
- Add/modify: `tools/claude_code_runtime_canary_config.py` if needed.
- Add tests for dry-run behavior if script is added.

This task may run before Task 6 only because it creates disabled placeholders. It must not enable bridge live routing.

- [ ] **Step 4.1: Write dry-run test/spec**

Required placeholder group names:

```text
zhumeng-claude-code-bridge-openai
zhumeng-claude-code-bridge-deepseek
zhumeng-claude-code-bridge-agnes
zhumeng-claude-code-bridge-anthropic-compat
future placeholders: zhumeng-claude-code-bridge-glm, zhumeng-claude-code-bridge-kimi
```

Expected properties before Task 6 passes:

```text
codex_gateway_entitled=false
augment_gateway_entitled=false unless explicitly needed later
formal_pool_allowed=false
models_list_config.enabled=false or equivalent live-disabled flag
no live upstream account binding
no membership in native group 8
```

- [ ] **Step 4.2: Implement idempotent dry-run / disabled-placeholder script**

Script requirements:

- dry-run by default;
- no secrets printed;
- no live bridge upstream account bindings before Task 6 passes;
- no modification to Codex Gateway groups;
- no bridge account in group 8;
- no `models_list_config.enabled=true` before route contract is green.

- [ ] **Step 4.3: Run dry-run**

Run:

```bash
python3 tools/claude_code_runtime_canary_config.py --dry-run --target http://127.0.0.1:3017
```

Expected: planned placeholder changes only, no secrets.

- [ ] **Step 4.4: Apply disabled placeholders only after dry-run review**

Apply only if it creates disabled placeholder groups with no live upstream bindings and no enabled bridge models. If DB sharing means even disabled placeholders are unacceptable, stop and ask user.

- [ ] **Step 4.5: Verify safe DB snapshot**

Use the redacted snapshot query from Task 0.4. Expected:

- bridge placeholder groups exist but are disabled/non-live;
- Codex Gateway groups unchanged;
- no bridge account in group 8;
- no bridge upstream account live-bound before Task 6 passes.

- [ ] **Step 4.6: Commit script/docs/tests**

```bash
git add tools/claude_code_runtime_canary_config.py <tests/docs>
git commit -m "feat: prepare disabled claude code bridge pool placeholders"
```

---

## Task 5A: Control-Plane Classification Matrix Design Gate

No control-plane code change may proceed until this task produces a reviewed, committed matrix. This is required because `18080` is the production-side Sub2API + CC Gateway stack with real formal-pool accounts, and because local Claude Code control-plane safety depends on a path-by-path contract with that server-side stack.

**Files:**
- Create/modify: `docs/anti-ban/47-claude-code-control-plane-classification-matrix.md`
- Possible create: `tools/claude_code_control_plane_matrix.py` (safe scanner only; no raw output)
- Possible tests: `tools/tests/test_claude_code_control_plane_matrix.py`

- [ ] **Step 5A.1: Build a sanitized evidence index**

Compare docs 30/35/38/45/46/47, safe capture deliverables/fragments for Claude Code CLI 2.1.150/2.1.175/2.1.177 where available, current Python guard policy, current Go control-plane router, and CC Gateway main policy. Output only source file, declared capture version, method, path template, host bucket, header names, query key names, body-size bucket, allowlisted schema key names, and expected action. Do not output raw body, raw query values, Authorization, `x-api-key`, cookies, CCH, prompt/messages, telemetry payloads, email, account/org/user UUIDs, proxy credentials, or deterministic body hashes.

- [ ] **Step 5A.2: Write the matrix document**

The matrix document must include:

1. Scope and assumptions: `3012/3017` are local development/testing; `18080` is production-side Sub2API + CC Gateway with real formal-pool accounts.
2. Evidence inventory for 2.1.150, 2.1.175, and 2.1.177 where available; missing evidence becomes `evidence_gap` and keeps that row blocked/stubbed.
3. Every required matrix column from the baseline.
4. Rows for: native messages, native count_tokens, event_logging v2/legacy, eval, bootstrap/hello/oauth hello, Claude Code feature flags, OAuth account settings, OAuth org/referral, Claude Code organizations, MCP public registry, MCP private servers, web/domain info, policy limits, remote managed settings, model capabilities, GrowthBook/feature gates, settings sync, team memory sync, official-domain CONNECT/non-Anthropic telemetry egress, unknown drift.
5. A "server-side CC Gateway cooperation" section for each family: selected account, persona/profile, egress bucket, `x-cc-*`/gateway-to-gateway proof, response schema verification, synthetic telemetry status, and fail-closed behavior.
6. A "not live yet" section for synthetic telemetry: schema registry, localhost replay, single-event real-pool approval, gray rollout, kill switch.
7. A pool-isolation section for native formal-pool, Claude Code bridge pools, and Codex Gateway pools: allowed client type, route hint, account/group binding, model catalog visibility, audit namespace, and forbidden cross-use.
8. Fixture list and targeted tests for every row.

- [ ] **Step 5A.3: Review the matrix before code**

Self-check:

```text
no row raw-uploads telemetry/eval
no row forwards local user Authorization/cookie/x-api-key
no row sends local x-sub2api-* headers upstream as ordinary passthrough
safe GET rows have schema allowlist + cache scope + partition keys
unknown paths fail closed
18080 live canary requires matrix row + localhost/mock replay + explicit approval
bridge rows cannot enter native formal-pool
Codex Gateway groups are not reused for Claude Code bridge/native pools
```

Then dispatch one read-only quality review agent for the matrix only. Do not run tests/builds in parallel. Fix any P0/P1 review issues.

- [ ] **Step 5A.4: Commit the design gate**

```bash
git add docs/anti-ban/47-claude-code-control-plane-classification-matrix.md tools/claude_code_control_plane_matrix.py tools/tests/test_claude_code_control_plane_matrix.py
git commit -m "docs: define claude code control-plane classification matrix"
```

If no scanner/test file is created, add only the matrix doc.

## Task 5B: Implement Control-Plane Classification From the Matrix

**Files:**
- Modify: `tools/cli_control_plane_policy.py`
- Modify: `tools/cli_control_plane_guard.py`
- Modify tests: `tools/tests/test_cli_control_plane_policy.py`, `tools/tests/test_cli_control_plane_guard.py`, `tools/tests/test_cli_control_plane_network_safety.py`, `tools/tests/test_cli_control_plane_intent.py`
- Modify: `backend/internal/service/control_plane_policy.go`
- Modify: `backend/internal/service/control_plane_path_matrix_config.go` only if the matrix schema needs new fields.
- Modify tests: `backend/internal/service/control_plane_policy_test.go`, `backend/internal/service/control_plane_path_matrix_config_test.go`, `backend/internal/service/control_plane_intent_test.go`, `backend/internal/service/control_plane_cache_test.go`, `backend/internal/service/control_plane_quarantine_test.go`
- Modify: `backend/internal/service/gateway_cc_gateway_control_plane_test.go`
- CC Gateway files only if a separate CC Gateway worktree has been created and Task 5A identifies a required CC Gateway change.

- [ ] **Step 5B.1: Write route policy tests from the matrix**

Cover launch actions exactly for messages, count_tokens, telemetry, eval, bootstrap/hello/oauth hello, Claude Code feature flags, organizations/org/referral, account settings, MCP registry, MCP servers, web/domain info, policy limits, remote settings, model capabilities, GrowthBook/feature gates, settings sync, team memory, official-host CONNECT, and unknown drift. Also assert safe intent output contains no raw secret, raw body, raw telemetry, prompt/messages, CCH, email, account/org/user UUID, path dynamic identifiers, or deterministic raw-body digest.

- [ ] **Step 5B.2: Implement Python guard policy**

In `cli_control_plane_policy.py` and `cli_control_plane_guard.py`: match method + host bucket + path template; generate safe intent with only matrix-approved fields; suppress/stub/block per matrix; strip local credentials; discard raw telemetry/eval bodies immediately after safe schema extraction; route count_tokens separately from messages if matrix requires a startup stub; keep live remote control-plane upload disabled until explicit approval.

- [ ] **Step 5B.3: Implement/align Go server matrix**

In Go control-plane policy files: align default rows with the committed matrix; reject unsafe raw/dynamic query values; enforce cache scope, TTL, partition keys, stale policy, and response allowlist; quarantine schema drift and private fields; ensure control-plane cannot enter messages/CCH signer or native formal-pool routing by accident; ensure telemetry/eval safe intent cannot include raw body hash/digest.

- [ ] **Step 5B.4: Add CC Gateway comparison tests or stop at CC Gateway worktree gate**

If Task 5A says CC Gateway must change for formal-pool control-plane fetch/synthetic telemetry, create the separate CC Gateway worktree first, add tests there for route policy/header/persona/no messages CCH reuse, keep synthetic telemetry off by default, and do not deploy or point `3017`/`18080` to changed CC Gateway without user approval. If no CC Gateway code change is required for launch, document that launch uses local stub/suppress/cache only for unsupported control-plane families.

- [ ] **Step 5B.5: Run targeted Python tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
python3 -m pytest tools/tests/test_cli_control_plane_policy.py -q
python3 -m pytest tools/tests/test_cli_control_plane_guard.py -q
python3 -m pytest tools/tests/test_cli_control_plane_network_safety.py -q
python3 -m pytest tools/tests/test_cli_control_plane_intent.py -q
```

Expected: PASS.

- [ ] **Step 5B.6: Run targeted Go tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'ControlPlanePolicy|ControlPlanePathMatrix|ControlPlaneIntent|ControlPlaneCache|ControlPlaneQuarantine|CCGatewayControlPlane' -count=1
```

Expected: PASS.

- [ ] **Step 5B.7: Commit**

```bash
git add tools/cli_control_plane_policy.py tools/cli_control_plane_guard.py tools/tests/test_cli_control_plane_policy.py tools/tests/test_cli_control_plane_guard.py tools/tests/test_cli_control_plane_network_safety.py tools/tests/test_cli_control_plane_intent.py backend/internal/service/control_plane_policy.go backend/internal/service/control_plane_path_matrix_config.go backend/internal/service/control_plane_policy_test.go backend/internal/service/control_plane_path_matrix_config_test.go backend/internal/service/control_plane_intent_test.go backend/internal/service/control_plane_cache_test.go backend/internal/service/control_plane_quarantine_test.go backend/internal/service/gateway_cc_gateway_control_plane_test.go
git commit -m "feat: classify claude code control-plane from matrix"
```

## Task 6: Route Contract Before Bridge Live Enablement

**Files:**
- Modify: `tools/claude_code_route_trust.py`
- Modify: `tools/cli_control_plane_guard.py`
- Modify tests: `tools/tests/test_cli_control_plane_guard.py`, `tools/tests/test_cli_control_plane_policy.py`
- Modify: `backend/internal/service/claude_code_native_route_admission_test.go`

- [ ] **Step 6.1: Add fail-closed tests**

Cover:

- body model native but route hint bridge -> fail closed;
- body model bridge but route hint native -> fail closed;
- bridge model forges `x-sub2api-client-type=claude_code_native` -> fail closed;
- missing hint for bridge model before CP4 live -> fail closed or mock-only;
- stale/replayed hint -> fail closed;
- backend catalog says bridge while client hint says native -> backend denies formal pool.

- [ ] **Step 6.2: Keep bridge live disabled by default**

Route catalog defaults may show bridge overlay proof, but live bridge models remain disabled unless explicitly listed in a canary feature flag after these tests pass.

- [ ] **Step 6.3: Run targeted tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
python3 -m pytest tools/tests/test_cli_control_plane_guard.py tools/tests/test_cli_control_plane_policy.py -q
cd backend && go test ./internal/service -run 'ClaudeCodeNativeAdmission|RouteHint|FormalPool' -count=1
```

Expected: PASS.

- [ ] **Step 6.4: Commit**

```bash
git add tools/claude_code_route_trust.py tools/cli_control_plane_guard.py tools/tests/test_cli_control_plane_guard.py tools/tests/test_cli_control_plane_policy.py backend/internal/service/claude_code_native_route_admission_test.go
git commit -m "test: enforce claude code native bridge route contract"
```

---

## Task 7: Canary Startup Validation on 3017

**Files:**
- Docs/evidence only unless bugs are found.

- [ ] **Step 7.1: Build canary binary**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o ../artifacts/bin/sub2api-current-linux-arm64 ./cmd/server
```

Expected: build succeeds.

- [ ] **Step 7.2: Restart only 3017**

Restart only the canary container with the new binary/config. Verify:

```bash
docker ps --format 'table {{.Names}}\t{{.Ports}}\t{{.Status}}' | rg '3012|3017'
```

Expected: `3012` still running; `3017` healthy.

- [ ] **Step 7.3: Use canary-only state path**

If refreshing managed device token/session is necessary, write state under worktree artifacts, for example:

```text
artifacts/claude-code-canary/state.json
```

Do not overwrite the user's normal Zhumeng state unless user explicitly asks. Do not print token values. Do not stage token state.

- [ ] **Step 7.4: Guard startup smoke**

Verify:

- guard ready;
- Claude Code base URL points to loopback guard;
- guard upstream points to 3017;
- local native attestation goes to 3017 only;
- no official messages egress from local CLI.

- [ ] **Step 7.5: Minimal native formal-pool smoke**

Use minimal prompt and safe evidence only:

```text
local guard route=messages
client_type=claude_code_native
remote route=cc_gateway/native_messages or approved equivalent
status code bucket
request id refs only
```

Expected: no remote `Invalid Claude Code native attestation` caused by local attestation leakage.

- [ ] **Step 7.6: Bridge no-pollution smoke**

Before bridge live is explicitly enabled, selecting a bridge model must:

- use mock/stub path; or
- fail closed with clear message;
- never enter group 8 / formal-pool native path.

- [ ] **Step 7.7: Document evidence**

Update `docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md` or an appendix with:

- canary ports;
- group names/IDs;
- safe test results;
- remaining risks;
- no secrets.

- [ ] **Step 7.8: Commit docs/evidence**

```bash
git add docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md
git commit -m "docs: record claude code runtime canary trust evidence"
```

---

## Task 8: Final Targeted Regression Matrix

**Files:** none unless regressions are found.

- [ ] **Step 8.1: Python tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
python3 -m pytest \
  tools/tests/test_cli_control_plane_policy.py \
  tools/tests/test_cli_control_plane_guard.py \
  tools/tests/test_cli_control_plane_network_safety.py \
  tools/zhumeng-agent/tests/test_claude_code_launcher.py \
  tools/zhumeng-agent/tests/test_cli.py \
  -q
```

Expected: PASS.

- [ ] **Step 8.2: Go targeted tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'ClaudeCode|Native|Guard|RouteHint|FormalPool|CCGateway|AnthropicAPIKeyPassthrough|ControlPlane|Compat|CodexGateway' -count=1
go test ./internal/handler -run 'ClaudeCode|Native|CountTokens|Gateway' -count=1
go test ./internal/server/routes -run 'ClaudeCode|Gateway|OpenAI' -count=1
go test ./internal/server/middleware -run 'ManagedDeviceOrAPIKeyAuth' -count=1
go test ./internal/repository -run 'APIKeyRepository_GetByID_LoadsUserAllowedGroups|MessagesDispatch' -count=1
```

Expected: PASS.

- [ ] **Step 8.3: CC Gateway tests only if CC Gateway worktree changed**

Run in CC Gateway worktree only, serially, using the repo's package scripts.

- [ ] **Step 8.4: Codex Gateway no-regression targeted check**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime/backend
go test ./internal/service -run 'CodexGateway|DeepSeek|OpenAIResponses|AGNES|Capture' -count=1
```

Expected: PASS.

- [ ] **Step 8.5: Provide user startup command**

Only after the previous gates pass, provide the actual supported command from the codebase. If state path is needed, use canary state:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
ZHUMENG_CLAUDE_CODE_API_BASE_URL=http://127.0.0.1:3017 \
ZHUMENG_CLAUDE_CODE_PROFILE=managed-runtime-canary \
ZHUMENG_AGENT_STATE_PATH=artifacts/claude-code-canary/state.json \
python3 -m zhumeng_agent.cli zhumeng-claude start --profile managed-runtime-canary
```

Do not include secrets on the command line.

---

## Task 9: Final Report

Report:

- Sub2API worktree and branch;
- commit chain;
- whether CC Gateway worktree was created;
- CC Gateway branch/commits if any;
- tests run;
- canary port used;
- startup command;
- remaining risks.

Explicitly state:

```text
3012 was not stopped/rebuilt.
main worktree dirty files were not touched.
production-visible account 132 was not edited in place while DB was shared.
Claude formal-pool native and bridge groups are isolated.
Local native attestation is not ordinary upstream passthrough material.
Bridge live remains disabled until route trust contract is green and user-approved.
```

---

## Final Review Checklist

- [ ] `x-sub2api-native-attestation` does not appear in ordinary Anthropic passthrough outbound requests.
- [ ] Generic local `x-sub2api-*` headers are stripped from ordinary upstream passthrough.
- [ ] CC Gateway-bound requests use `x-cc-*` and do not forward local internal headers.
- [ ] Formal-pool account cannot silently fall back to ordinary passthrough when CC Gateway config is incomplete.
- [ ] Existing production-visible account `132` was not edited in place while 3017 shares DB with 3012.
- [ ] Bridge placeholders are disabled/non-live before route contract is green.
- [ ] Bridge model selection cannot enter formal-pool native path.
- [ ] Control-plane classification matrix is committed before control-plane code changes.
- [ ] Control-plane unknown drift blocks/quarantines.
- [ ] High-risk telemetry/eval raw bodies never leave local guard and never get plain/deterministic hashes.
- [ ] Safe GET control-plane rows have schema allowlists, cache TTL, partition keys, and stale policy.
- [ ] Any live control-plane canary against `18080` has matrix row + localhost/mock replay + explicit approval because it has real formal-pool accounts.
- [ ] Startup count_tokens probe does not break native CLI startup.
- [ ] No official `api.anthropic.com` direct messages egress from local CLI.
- [ ] No raw secrets/prompt/body/telemetry/CCH appear in logs, docs, commits, or final report.
