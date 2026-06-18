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
- Treat the current Postgres database as shared by `3012` and `3017` until proven otherwise.
- Do **not** mutate existing production-visible records such as account `132` in place while the DB is shared.
- Canary DB changes must use disabled placeholders, cloned canary-only records, or 3017-only feature gates. Anything that can affect `3012` requires explicit user confirmation.
- Do not print or commit API keys, tokens, refresh tokens, credentials, cookies, raw prompts, raw bodies, raw telemetry, raw CCH, or raw provider responses.
- Do not read/print raw shared live config file lines. If config shape must be inspected, output only field presence or redacted lengths using a sensitive-key allow/deny list.
- Run tests/builds serially, not in parallel.
- Do not delete files/directories, run `git reset`, `git clean`, `git rebase`, `git checkout --`, `git restore`, `sudo`, `chmod -R`, or `chown -R` without user confirmation.
- If CC Gateway code must change, first create a CC Gateway worktree from `/Users/muqihang/chelingxi_workspace/cc-gateway` at `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-runtime-formal-pool-bridge-repair` on branch `codex/claude-code-runtime-formal-pool-bridge-repair`, then index it with CodeGraph. Do not edit CC Gateway main.

---

## Current Facts to Preserve

1. `3017` currently runs a canary Sub2API binary but appears to use the same Postgres/data volume as `3012`.
2. DB has `zhumeng-claude-code-native` group, but no Claude Code bridge-specific groups yet.
3. Account `132` (`zhumeng-claude-code-native-upstream`) is currently an ordinary Anthropic API-key passthrough to `http://198.12.67.185:18080`. Do not edit it in place while DB is shared.
4. Local app-level `gateway.ccgateway.enabled/base_url/token/providers.anthropic` is not currently configured for 3017.
5. Current `shouldForwardClientHeaderToAnthropic` forwards local Claude Code native attestation/signature/internal headers; this conflicts with docs 46/47.
6. CC Gateway sub2api mode expects `x-cc-gateway-token`, selected account ref, provider, token type, policy version, egress bucket, and trusted persona/context headers. It should not trust local guard attestation headers.
7. Lab captures show control-plane families beyond `/v1/messages`: `count_tokens`, event logging, eval, bootstrap, penguin mode, MCP registry, organizations, and web domain info.

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

- `docs/anti-ban/47-claude-code-multiprovider-runtime-completion-audit.md`
  - Record safe evidence, tests, and remaining risks.

### CC Gateway, only if required

- Create separate worktree before edits.
- Likely files if needed: `src/proxy.ts`, `src/policy.ts`, `src/rewriter.ts`, and corresponding tests.

---

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

## Task 2: Decide Remote Formal-Pool Ingress Type

**Files:** none unless docs are updated later.

- [ ] **Step 2.1: Probe remote endpoint type safely**

Use safe health-style probes only; do not send prompt/body/key values:

```bash
curl -sS -i http://198.12.67.185:18080/_health | sed -n '1,20p'
curl -sS -i http://198.12.67.185:18080/health | sed -n '1,20p'
```

Expected: identify whether remote looks like CC Gateway or Sub2API.

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

- [ ] **Step 2.3: Choose one branch**

Decision rules:

- If remote is CC Gateway-compatible and mandatory gateway token/config are available: use Task 3A with canary-only cloned records, not account `132`.
- If remote is Sub2API and cannot accept CC Gateway headers: stop at Task 3B design gate; no implementation/deployment without user approval.
- If remote requires CC Gateway code changes: perform Task 2.4 before any CC Gateway edit.
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

## Task 3A: Wire Local Sub2API to Remote CC Gateway Boundary

Use only if Task 2 proves remote accepts CC Gateway-compatible ingress.

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

- [ ] **Step 3A.3: Configure 3017 app-level CCGateway settings only in canary runtime**

Do not edit shared raw config in a way that affects `3012`. Prefer canary container environment or a 3017-only config overlay.

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

## Task 3B: Stop-and-Design Gate for Remote Sub2API Ingress

Use only if Task 2 proves remote is Sub2API and cannot accept CC Gateway headers. This is not automatic implementation.

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

Do not implement/deploy server-to-server ingress until user approves the remote-side plan. If not approved, report that remote must expose a CC Gateway-compatible ingress for this local rollout.

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

## Task 5: Implement Control-Plane Classification

**Files:**
- Modify: `tools/cli_control_plane_policy.py`
- Modify: `tools/cli_control_plane_guard.py`
- Modify tests: `tools/tests/test_cli_control_plane_policy.py`, `tools/tests/test_cli_control_plane_guard.py`, `tools/tests/test_cli_control_plane_network_safety.py`

- [ ] **Step 5.1: Write route policy tests**

Cover:

```text
POST /v1/messages?beta=true -> forward_messages
POST /v1/messages without beta query but with anthropic-beta header -> explicit decision tested
POST /v1/messages/count_tokens -> local_probe_stub or safe_count_tokens
POST /api/event_logging/v2/batch -> suppress_shadow
POST /api/event_logging/batch -> suppress_shadow
POST /api/eval/* -> suppress_shadow
GET/POST /api/claude_cli/bootstrap -> stub_safe_intent
GET /api/claude_code_penguin_mode -> stub_safe_intent
GET /mcp-registry/v0/servers -> public_registry_stub_or_cache
/api/claude_code/organizations/{org} -> stub_safe_intent
/api/web/domain_info -> stub_safe_intent
settings/team_memory/sync -> block_shadow
unknown -> block_quarantine
```

Also assert safe intent output contains no raw secret, raw body, raw telemetry, or prompt text.

- [ ] **Step 5.2: Implement policy matrix**

In `cli_control_plane_policy.py`, match method + host bucket + path template and return:

- action;
- raw forbidden flag;
- cache scope and TTL when applicable;
- quarantine behavior for unknown.

- [ ] **Step 5.3: Ensure guard emits safe intent only**

Safe fields only:

```text
route template
method
host bucket
header names
auth presence shape
body size bucket
schema summary
event name enum if safely extractable
action/status
```

- [ ] **Step 5.4: Run targeted Python tests**

Run serially:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-multiprovider-runtime
python3 -m pytest tools/tests/test_cli_control_plane_policy.py -q
python3 -m pytest tools/tests/test_cli_control_plane_guard.py -q
python3 -m pytest tools/tests/test_cli_control_plane_network_safety.py -q
```

Expected: PASS.

- [ ] **Step 5.5: Commit**

```bash
git add tools/cli_control_plane_policy.py tools/cli_control_plane_guard.py tools/tests/test_cli_control_plane_policy.py tools/tests/test_cli_control_plane_guard.py tools/tests/test_cli_control_plane_network_safety.py
git commit -m "feat: classify claude code control-plane safely"
```

---

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
- [ ] Control-plane unknown drift blocks/quarantines.
- [ ] Startup count_tokens probe does not break native CLI startup.
- [ ] No official `api.anthropic.com` direct messages egress from local CLI.
- [ ] No raw secrets/prompt/body/telemetry/CCH appear in logs, docs, commits, or final report.
