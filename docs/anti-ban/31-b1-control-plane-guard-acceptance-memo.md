# B1 Control-Plane Guard Acceptance Memo

Date: 2026-05-23
Scope: localhost-only / mock-only B1 acceptance
Status: ACCEPTED for B1; not approved for real canary

## 1. Acceptance conclusion

B1 control-plane guard is accepted for localhost-only / mock-only scope.

Verified results:

- Checkpoint 1-5: PASS
- Python required suite: PASS, 68 tests
- Go targeted tests: PASS
- CC Gateway build: PASS
- CC Gateway test run 1: PASS, 69 tests
- CC Gateway test run 2: PASS, 69 tests
- Dual-scenario full-chain controller: PASS
- Sensitive scan: PASS
- Real Anthropic/Claude upstream access: false
- Real `/v1/messages` canary: false
- Login: false
- git add/commit: false

Safe deliverable:

- `/tmp/b1-control-plane-guard-implementation-readiness-20260523/report.md`
- `/tmp/b1-control-plane-guard-implementation-readiness-20260523/report.json`
- `/tmp/b1-control-plane-guard-implementation-readiness-20260523/sensitive-scan.json`

## 2. What B1 proves

B1 proves the local control-plane guard can safely classify and contain Claude Code CLI side traffic in localhost-only mode.

Specifically:

- known control-plane routes can be stubbed, suppressed, or blocked locally;
- unknown routes fail closed;
- CONNECT does not become a real tunnel;
- local Claude Code auth material is stripped and not persisted;
- normalized intent envelope and redaction rules exist;
- future formal-upload fields are present only as disabled/no-op placeholders;
- unsafe/default-like CLI envelopes are blocked by local cost gates;
- safe lite/mock messages can reach localhost CC Gateway mock;
- stale artifact protection is in place through unique run directories;
- safe artifacts and raw-sensitive artifacts have separate archive paths.

## 3. What B1 does not prove

B1 does not prove production readiness and does not authorize real upstream traffic.

Not proven:

- real Anthropic `/v1/messages` behavior;
- real control-plane upload behavior;
- account-scoped control-plane passthrough;
- account-scoped cached fetch;
- public registry fetch;
- sanitized synthetic telemetry;
- production session-budgeted pool behavior;
- long-running multi-user shared-account isolation;
- whether missing/stubbed bootstrap/settings/MCP/telemetry affects upstream risk over time.

## 4. Full-chain localhost-only outcome

Scenario A: unsafe/default-like envelope

- result: PASS
- cost envelope block: true
- mock request count: 0
- interpretation: local block is expected and safe for this scenario

Scenario B: safe lite/mock envelope

- result: PASS
- route: `POST /v1/messages?beta=true`
- model: `claude-sonnet-4-6`
- max_tokens: 128
- body size: 532 bytes
- tools_count: 0
- mock request count: 1
- billing marker / CCH shape: present in localhost mock evidence

## 5. Remaining issues

P0: none
P1: none
P2: Python unittest ResourceWarning cleanup warnings; non-blocking.
Unknown: none for B1 localhost-only scope.

## 6. Next-stage decision boundary

Do not proceed directly to real canary from B1 alone.

Before any next real request, require a separate approval and a narrow candidate plan covering:

1. exact route and request count;
2. model and beta profile;
3. cost envelope;
4. whether request is real CLI-through or controlled lite fixture;
5. proxy/egress verification;
6. raw-sensitive archive policy;
7. safe deliverable policy;
8. stop conditions;
9. no retry / no fallback / no second request gates.

## 7. Recommended next step

Recommended next step is planning, not execution:

- prepare a single-request real CLI-through canary proposal, or
- prepare a further localhost-only rehearsal if the real candidate still depends on CLI default behavior.

Given B1 showed real CLI default-like envelopes may exceed canary limits, the next real candidate should not silently use the default large envelope. It must either:

- use a proven low-cost request shape; or
- deliberately test the local cost gate only, without real upstream.

## 8. Continuing prohibitions

Until separately approved:

- no real `/v1/messages`;
- no Anthropic/Claude real domain access;
- no control-plane real upload;
- no login;
- no git add/commit;
- no raw token/auth/body/prompt/CCH in safe deliverables;
- no production pool rollout.
