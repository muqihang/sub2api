# 逐梦 Agent Claude Code Native Takeover CP8 Final Validation Memo

日期：2026-06-09
分支：`codex/claude-code-native-takeover`
工作目录：`/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-native-takeover`
边界：localhost/mock only；未发送真实 Anthropic/Claude 请求。

## 1. Checkpoint commit ledger

| Checkpoint | Commit | 摘要 |
| --- | --- | --- |
| CP0 | `e07a92d9b` | 冻结 native takeover baseline memo |
| CP1 | `8220234f8` | Claude Code native adapter skeleton |
| CP2 | `9ecd464a2` | Loopback guard product wrapper |
| CP3 | `2e379e148` | Process netwatch v2 |
| CP4 | `79387b47e` | Capability profile and ToolSearch strategy |
| CP5 | `0d655d387` | Native attestation markers and backend integration |
| CP6 | `afab3adea` | Native shape healthcheck fixtures |
| CP7 | `cdc92f66f` | Operator status and runbook |

## 2. CP8 validation commands and results

### 2.1 Worktree hygiene and sensitive scan

```bash
git status --short --branch
# ## codex/claude-code-native-takeover

python3 tools/safe_deliverable_sensitive_scan.py --max-findings 100
# files_scanned=57
# findings=0

git diff --check
# PASS, no output
```

### 2.2 Forbidden path / parallel-worktree conflict check

```bash
git diff --name-only main..HEAD | rg '^(backend/internal/service/codex_gateway_|backend/internal/pkg/apicompat/|docs/codex-gateway/)' || true
# PASS, no output

git diff --name-only main..HEAD | rg '(deepseek|agnes|codex[-_ ]desktop[-_ ]gateway)' -i || true
# PASS, no output
```

No modified files under the explicitly forbidden `codex_gateway_*`, `apicompat`, or `docs/codex-gateway` scopes. No DeepSeek / AGNES / Codex Desktop Gateway specialty scope was changed.

### 2.3 Python / Agent tests

```bash
cd tools/zhumeng-agent
UV_CACHE_DIR=/private/tmp/uv-cache uv run --python /opt/homebrew/bin/python3   --with pytest --with pytest-asyncio python -m pytest tests -q
# 309 passed, 15 warnings in 65.11s
```

The warnings are existing aiohttp `NotAppKeyWarning` messages in proxy tests; no test failure.

### 2.4 Go targeted tests

```bash
cd backend
go test ./internal/service ./internal/handler ./internal/server/routes   -run 'ClaudeCode|Native|Guard|ControlPlane|Compat|Shape|Gateway|Session|Account'   -count=1 -timeout=240s
# ok github.com/Wei-Shaw/sub2api/internal/service 45.971s
# ok github.com/Wei-Shaw/sub2api/internal/handler 11.702s
# ok github.com/Wei-Shaw/sub2api/internal/server/routes 3.003s
```

### 2.5 CC Gateway build/tests

Before running, `/Users/muqihang/chelingxi_workspace/cc-gateway` was checked read-only:

```bash
git status --short --branch
# ## main...origin/main [ahead 19]
# ?? .claude/
# ?? .worktrees/
```

Only build/tests were run; no files were edited or staged:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
npm run build
npm test -- --runInBand
git status --short --branch
# build PASS
# tests/run-all.ts: final total 106 passed, 0 failed
# ## main...origin/main [ahead 19]
# ?? .claude/
# ?? .worktrees/
```

The CC Gateway test harness used localhost mock upstreams and includes preflight fail-closed coverage for official-domain upstream configuration. No real upstream request was authorized or sent by this CP8 run.

## 3. Acceptance criteria evidence map

| # | Acceptance criterion | Evidence |
| ---: | --- | --- |
| 1 | 逐梦 Agent 能显式启动 isolated Claude Code CLI | CP1 launcher tests and CP2 guard integration tests in `tools/zhumeng-agent/tests/test_claude_code_launcher.py` and `test_claude_code_guard.py`; Python suite PASS. |
| 2 | `ANTHROPIC_BASE_URL` 指向 loopback guard | CP1 safe env builder and CP2 guard tests require loopback URLs; Python suite PASS. |
| 3 | 默认 `~/.claude` 不被修改、不读取、不上传 | CP1 isolated config path tests and runbook; sensitive scan findings=0. |
| 4 | messages 经过 guard -> Sub2API -> CC Gateway | CP2 guard forwarding tests plus CP5 backend native markers and CC Gateway boundary tests; Python/Go/CC Gateway suites PASS. |
| 5 | control-plane 分类 safe intent，不复用 messages signer | CP2 guard control-plane tests, CP5 path matrix tests, CP6 shape fixtures, CP7 operator fail-closed decision allowlist; Python/Go suites PASS. |
| 6 | process netwatch 可检测 guard bypass | CP3 netwatch implementation/tests and CP7 `guard_bypass` operator status; Python suite PASS. |
| 7 | ToolSearch / `tool_reference` / `defer_loading` 有 profile 管控和 healthcheck | CP4 profile/doctor tests and CP6 shape fixtures; Python suite PASS. |
| 8 | custom Base URL 不误关 1m、thinking、Opus/Sonnet、stream、tools、`max_tokens=32000` | CP4/CP6 profile and shape fixture tests; Python suite PASS. |
| 9 | `claude_code_native` 与 `claude_code_compat` 清晰区分 | CP5 backend native marker tests and compat separation tests; Go targeted suite PASS. |
| 10 | 不保存 raw sensitive | Safe summary/redaction tests, CP7 status raw-safe tests, final sensitive scan findings=0. |
| 11 | sensitive scan findings=0 | CP8 sensitive scan result: `findings=0`. |
| 12 | Python/Go/CC Gateway tests PASS | CP8 Python 309 passed; Go targeted packages ok; CC Gateway 106 passed, 0 failed. |
| 13 | 未经批准不发真实 Anthropic/Claude 请求 | CP8 run used localhost/mock only; CC Gateway official-domain path is fail-closed preflight coverage, not a real upstream call. |

## 4. CP7 review issue closure

GPT-5.5 CP7 review initially found one blocker: arbitrary `control_plane.decision` could still reach `ready`. CP7 was fixed before commit:

- `status.py` now has `_ALLOWED_CONTROL_PLANE_DECISIONS` and uses `_control_plane_decision_allowed()` in readiness, quarantine, and reason paths.
- Invalid decisions such as `direct_forward` produce `quarantined` with `control_plane_decision_not_allowed`.
- `tests/test_claude_code_status.py` includes a regression test for this fail-closed behavior.
- GPT-5.5 CP7 re-review returned `PASS_WITH_NOTES` and recommended stage+commit.

## 5. Safety statement

This CP8 validation did not read, upload, export, or copy default `~/.claude` OAuth/cookie/setup-token material. It did not store raw token, raw prompt, raw body, raw telemetry, raw CCH, email, account/org UUID, or proxy credentials. Control-plane evidence remains safe intent / suppress / stub / block / shadow only, with no reuse of messages signing. Direct official-domain bypass remains fail closed by guard/netwatch/status/preflight checks.
