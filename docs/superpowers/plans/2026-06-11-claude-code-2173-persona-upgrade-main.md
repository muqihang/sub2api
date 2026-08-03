# Claude Code 2.1.175 Persona Upgrade from Local Main Implementation Plan

> **2026-06-12 target revision:** Target CLI version was corrected from the previous target to **2.1.175** after Checkpoint 0 worktrees were created. The existing fresh local-main worktrees/branches named `claude-code-2173-main` are intentionally reused as implementation worktrees; the directory/branch names are historical and do not define the persona version. All version-sensitive evidence, code, docs, tests, safe-deliverable paths, and production persona constants must use pinned `2.1.175`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Starting from the current local `main` branch of `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main`, upgrade the verified Claude Code outbound persona from `2.1.150` to pinned `2.1.175` across Sub2API and the standalone CC Gateway so new Claude models such as `claude-opus-4-8` and `claude-fable-5` no longer look like they originate from an outdated CLI profile.

**Architecture:** Treat `2.1.175` as a newly verified profile, not a blind string replacement. First create a clean worktree from local `main`, collect sanitized evidence using an isolated `@anthropic-ai/claude-code@2.1.175`, then update Sub2API persona constants/header emission and standalone CC Gateway persona registry/resolver/signing/verifier paths. Preserve 2.1.150 as rollback evidence and keep fake/old client blocking intact.

**Tech Stack:** Go backend, Vue/TypeScript frontend only for already-existing model selector regression checks, Python Claude Code local capture tools, npm `@anthropic-ai/claude-code@2.1.175`, standalone TypeScript CC Gateway at `/Users/muqihang/chelingxi_workspace/cc-gateway`, Go tests, Python tests, Node tests, frontend Vitest/vue-tsc/build, production Docker/binary deployment.

---

## Local-main baseline facts

This plan intentionally ignores `origin/main`. The implementation baselines are the **current local `main`** branches in both repositories:

```text
/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
/Users/muqihang/chelingxi_workspace/cc-gateway
```

Observed local-main state on 2026-06-11:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
git branch --show-current
# main

git status --short
# clean at inspection time
```

Important: old plan commits in `.worktrees/claude-antiban-implementation` are **not** the implementation base. Use them only as historical reference if needed. The standalone CC Gateway implementation must also use a fresh worktree from `/Users/muqihang/chelingxi_workspace/cc-gateway` local `main`; do not edit that repo's main working tree directly.

### Current local-main anchors

Sub2API already contains native model catalog work for the new models:

- `backend/internal/pkg/claude/constants.go`
  - `DefaultModels` already includes `claude-fable-5` and `claude-opus-4-8`.
  - `CLICurrentVersion` is still `"2.1.150"`.
  - `ClaudeCodeMessagesOAuthBetasForBody` comment still says observed CLI `2.1.150`.
- `backend/internal/pkg/claude/constants_test.go`
  - `TestClaudeCodePersonaDefaultsTrack2150` asserts `2.1.150`.
  - Tests already cover `claude-opus-4-8` and `claude-fable-5` in `DefaultModels`.
- `backend/internal/service/cc_gateway_adapter.go`
  - `ccGatewayAnthropicPolicyVersion = "2.1.150"`.
  - `applyCCGatewayAnthropicHeaders` currently writes `x-cc-policy-version` from `account.Extra["cc_gateway_policy_version"]`.
  - `applyCCGatewayAnthropicPolicyVersion` can also write trusted inbound `GetClaudeCodeVersion(ctx)` or account Extra.
  - `ccGatewayPolicyVersionCompatible` uses same-minor compatibility around the anchored policy version.
- `backend/internal/service/formal_pool_onboarding_service.go`
  - Builds `PolicyVersion` and `PersonaVariant` from `ccGatewayAnthropicPolicyVersion`.
- `backend/internal/service/identity_service.go`
  - Comment and default synthesized fingerprint refer to Claude Code `2.1.150`.
- `backend/internal/service/local_capture_acceptance_artifact_test.go`
  - Constants `jointExpectedGatewayUserAgent` and `jointExpectedGatewayPersonaVariant` are `2.1.150`.
- `backend/internal/handler/gateway_native_attestation_test.go`
  - Test fixture uses `claude_code_version: 2.1.150`.
- `backend/internal/handler/gateway_helper_hotpath_test.go`
  - Several 2.1.150 context/version assertions exist.
- `tools/claude-code-lab-start.sh`
  - Already passes through a command after `--`; do **not** add a second `--` in examples.
- `tools/claude_code_lab_capture.py`
  - Already supports arbitrary command passthrough.
  - `_safe_command_display()` currently returns the command mostly verbatim; this is acceptable only if test prompts/secrets are not passed as argv. If future capture uses `--print <prompt>`, redact prompt-like argv first.
- Frontend `frontend/src/composables/useModelWhitelist.ts`
  - Already includes `claude-opus-4-8` and `claude-fable-5`.
  - This plan should not redo model catalog/pricing unless a regression is found.

Standalone CC Gateway local-main anchors in `/Users/muqihang/chelingxi_workspace/cc-gateway`:

- Local branch should be verified as `main` before creating the implementation worktree.
- Current main worktree may contain untracked `.claude/` and `.worktrees/`; these are not part of this plan and must not be staged.

- `src/persona-registry.ts`
  - Contains `claude_code_2_1_146`, `claude_code_2_1_150_subscription`, `claude_code_2_1_150_subscription_1m`.
  - `KNOWN_MODELS` includes `claude-opus-4-8`; verify/add `claude-fable-5`.
  - `inferLegacyPersonaVariant()` falls back to `2.1.150` or `2.1.146` profiles.
- `src/persona-resolver.ts`
  - Fallback currently uses `2.1.150`.
  - `fallbackProfile()` returns `claude_code_2_1_150_subscription_1m`.
- `src/policy.ts`
  - Builds `User-Agent`, `anthropic-beta`, signed `cc_version`, and verifier expectations from resolver profile/version.
- `src/proxy.ts`
  - Contains final forwarding/verifier checks and test fixtures that may assert signed `cc_version`/UA.
- `config.example.yaml`
  - Defaults document `version: "2.1.150"`, `version_base: "2.1.150"`, `message_beta_profile: claude_code_2_1_150_subscription_1m`, and account identity `policy_version: "2.1.150"`.
- `tests/`
  - Many exact assertions for 2.1.150 and legacy 2.1.146.

### Version-sensitive signing / fingerprint constants (CCH + cc_version)

The persona is not just a version string. The billing header `x-anthropic-billing-header: cc_version=X.Y.Z.<3hex>; cc_entrypoint=sdk-cli; cch=<5hex>;` is produced by deterministic algorithms whose constants are hard-coded. A CLI version bump only changes the *inputs* (version string fed into the suffix hash); it does **not** prove the *algorithm* is unchanged. These must be re-verified against real 2.1.175, not assumed.

Authoritative existing algorithm reference (already documented from prior reverse engineering):

- `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/cch-algorithm.md`
  - CCH = `lower_hex_5( xxh64(body_with_cch=00000_placeholder, SEED) & 0xFFFFF )`.
  - `cc_version` 3-hex suffix = `sha256( salt + chars + version )[:3]`, where `chars` are UTF-16LE indices `[4, 7, 20]` of the first non-`<system-reminder>` user text.

Current production constants to compare against (must match evidence or the plan scope escalates):

- Standalone CC Gateway `/Users/muqihang/chelingxi_workspace/cc-gateway/src/policy.ts` (authoritative signer):
  - `CCH_SEED = 0x4d659218e32a3268n`, `CCH_MASK = 0xfffffn` (xxh64 low-20-bits, 5 hex).
  - `CC_VERSION_SALT = '59cf53e54c78'`, `CC_VERSION_POSITIONS = [4, 7, 20]`, suffix = `sha256(salt + chars + cliVersion).slice(0, 3)`.
  - `computeCCH5Hex`, `computeCCVersionSuffix`, `runSigningPipeline`, `verifySignedCCH` define the full sign+verify path.
- Sub2API `backend/internal/service/gateway_billing_header.go`:
  - Has a legacy/experimental `cchSeed = 0x6E52736AC806831E` used by `signBillingHeaderCCH` (marked deprecated). Confirm whether any active normal path still uses this seed; if so it must agree with the authoritative CC Gateway seed or be retired.
  - `syncBillingHeaderVersion` rewrites `cc_version=` from the User-Agent version, so a UA bump to 2.1.175 already flows into the billing `cc_version` semver part.

### Stainless SDK / OS fingerprint headers

Beyond `User-Agent`, the real Claude Code CLI emits a Stainless SDK fingerprint set that is version-correlated and must be captured as evidence, not guessed:

- `x-stainless-package-version`, `x-stainless-runtime`, `x-stainless-runtime-version`, `x-stainless-os`, `x-stainless-arch`, `x-stainless-lang`, `x-stainless-retry-count`, `x-stainless-timeout` (exact set per real 2.1.175 capture).
- Persona variant is OS-scoped (`claude-code-2.1.x-macos-local`); capture machine OS must match the declared persona OS, or the evidence must explicitly record the OS used so `x-stainless-os` / UA platform segment stay consistent.

## Non-negotiable constraints

- Do **not** implement from `.worktrees/claude-antiban-implementation`; create fresh worktrees from each repo's local `main`.
- Do **not** reference or depend on `origin/main` for this task unless the user later says so.
- Do **not** send raw local Claude credentials, raw OAuth tokens, raw API keys, raw prompts, raw cookies, raw account email/UUID, or raw request bodies into git, docs, logs, or chat output.
- Do **not** commit the unpacked npm wrapper, the native `claude` binary, `strings`/disassembly output, raw source snippets, or any extracted Anthropic constant into git. Record only sanitized conclusions (algorithm unchanged yes/no, constant matches yes/no). Treat reverse-engineering output as local-only `/tmp` scratch.
- Do **not** treat a CLI version bump as proof the CCH / cc_version signing algorithm is unchanged. The algorithm (seed, salt, positions, hash function, masking, placeholder convention) must be re-verified byte-for-byte against real 2.1.175 before trusting it.
- Global Claude Code may be upgraded to pinned `2.1.175` if the local default `claude --version` is not `2.1.175 (Claude Code)`; record the upgrade command and result. Do **not** install floating `latest` into production code or make production follow npm dist-tags dynamically. Capture should record both global `claude --version` and pinned `npx -y -p @anthropic-ai/claude-code@2.1.175 claude --version`.
- Do **not** make production follow npm `latest` dynamically. Production must use explicit pinned verified `2.1.175`.
- Do **not** weaken existing spoof/fake-client blocks: `.test` versions, all-zero metadata, fake `cch=00000`, bad protocol, or non-Claude-Code compatibility guards must remain fail-closed.
- Do **not** redo new-model pricing/catalog work unless tests show the local main already regressed.
- Do **not** let stale production account Extra values such as `cc_gateway_policy_version=2.1.150` keep normal production traffic on an old final persona. Stale values may remain as DB metadata/diagnostics, but final trusted normal emission must canonicalize to verified `2.1.175`.
- Do **not** remove legacy 2.1.146/2.1.150 profiles/tests entirely. They are useful for downgrade, spoofing, and regression tests.
- Do **not** use destructive git commands (`git reset`, `git clean`, `git restore`, `git checkout --`, `git rebase`) without explicit user approval.
- Use exact `git add` paths. Do not use broad `git add .`, `git add backend`, `git add tools`, or `git add docs`.
- If delegating, use GPT-5.5 with reasoning `high`; wait at checkpoint boundaries and close finished subagents.

## Checkpoints

1. Checkpoint 0: Fresh worktree and preflight from local `main`.
2. Checkpoint 1: Safe local 2.1.175 evidence capture and diff.
3. Checkpoint 2: Sub2API persona upgrade to 2.1.175.
4. Checkpoint 3: Standalone CC Gateway persona upgrade to 2.1.175.
5. Checkpoint 4: Joint smoke/docs/rollout readiness.
6. Checkpoint 5: Production deployment and canary after user approval.

Each checkpoint must pass review before continuing. Commit after each implementation checkpoint that changes tracked files.

---

## Checkpoint 0: Fresh worktrees and preflight from local main

**Purpose:** Ensure implementation starts from local `main` in both repositories, not the old anti-ban worktree and not either repo's main working tree.

**Files:** none expected.

- [ ] **Step 0.1: Verify Sub2API local main worktree is clean**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
git branch --show-current
git status --short
git log --oneline -n 8
```

Expected:

- Branch is `main`.
- `git status --short` is clean or contains only unrelated pre-existing files/changes that are explicitly recorded; implementation must not edit either repo's main working tree.
- Do not inspect or rely on `origin/main` for the base decision.

- [ ] **Step 0.2: Verify/reuse the fresh Sub2API implementation worktree from local main**

This worktree was already created from local `main` before the target-version correction and is intentionally reused despite its historical `2173` name. Do not delete it or create a replacement unless provenance/branch/cleanliness is wrong and the user approves.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
git branch --show-current
git status --short --branch
git log --oneline -n 8
git merge-base --is-ancestor 23ce10c81 HEAD && git rev-parse HEAD
```

Expected:

- Branch is `codex/claude-code-2173-main`.
- HEAD is the local-main baseline commit recorded in Step 0.1 (`23ce10c81` unless Step 0.1 records a different local-main HEAD).
- Worktree has no unrelated tracked changes before the current checkpoint's planned edits.

If the worktree is missing, only then create it from local `main` with:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main
git worktree add .worktrees/claude-code-2173-main -b codex/claude-code-2173-main main
```

- [ ] **Step 0.3: Verify/reuse the standalone CC Gateway implementation worktree from local main**

This worktree was already created from local `main` before the target-version correction and is intentionally reused despite its historical `2173` name. Do not delete it or create a replacement unless provenance/branch/cleanliness is wrong and the user approves.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
git branch --show-current
git status --short
git log --oneline -n 8
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
git branch --show-current
git status --short --branch
git log --oneline -n 8
git merge-base --is-ancestor 3880f55 HEAD && git rev-parse HEAD
```

Expected:

- Source branch is local `main`.
- Untracked `.claude/` or `.worktrees/` in the CC Gateway main worktree are not staged or modified.
- Worktree branch is `codex/claude-code-2173-main`.
- Worktree HEAD is the CC Gateway local-main baseline commit recorded in Step 0.3 (`3880f55` unless the source local-main HEAD differs at verification time).
- Worktree is clean before the current checkpoint's planned edits.

If the worktree is missing, only then create it from local `main` with:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway
git worktree add .worktrees/claude-code-2173-main -b codex/claude-code-2173-main main
```

- [ ] **Step 0.4: Verify global and pinned 2.1.175 package availability**

Run:

```bash
npm view @anthropic-ai/claude-code version dist-tags --json
claude --version
npx -y -p @anthropic-ai/claude-code@2.1.175 claude --version
```

Expected:

```text
{ "version": "2.1.175", "dist-tags": { "latest": "2.1.175", "next": "2.1.175", "stable": "2.1.153" } }
2.1.175 (Claude Code)
2.1.175 (Claude Code)
```

If global `claude --version` is not `2.1.175 (Claude Code)`, the user has authorized a normal global upgrade to pinned `2.1.175`; run and record:

```bash
npm install -g @anthropic-ai/claude-code@2.1.175
claude --version
npm ls -g --depth=0 @anthropic-ai/claude-code
```

The target remains explicit pinned `2.1.175`; production code must not depend on npm `latest` dynamically.

- [ ] **Step 0.5: Checkpoint 0 review**

Reviewer verifies:

- Both implementation bases are local `main`: Sub2API and standalone CC Gateway.
- Neither repo's main working tree will be edited directly during implementation.
- No production code changed.
- If global `claude` was modified, it was upgraded only to pinned `@anthropic-ai/claude-code@2.1.175`, and the upgrade command plus `claude --version` / `npm ls -g` result were recorded.
- No secret values were printed or persisted.

No commit required for checkpoint 0.

Note: files under `docs/` are ignored by this repository's `.gitignore`; if this plan itself is committed, use `git add -f docs/superpowers/plans/2026-06-11-claude-code-2173-persona-upgrade-main.md`.

---

## Checkpoint 1: Safe local 2.1.175 capture and diff

**Purpose:** Collect enough sanitized evidence to justify a verified 2.1.175 profile.

**Files:**

- Potentially modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/tools/claude_code_lab_capture.py`
- Create if needed: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/tools/tests/test_claude_code_lab_capture.py`
  - This file does not exist on local `main`; create it only if `_safe_command_display()` needs direct unit coverage. Use the repo's existing `unittest` style.
- Potentially modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/tools/tests/test_cli_control_plane_guard.py`
- Create safe evidence summary only:
  - `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/README.md`
  - `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/evidence.json`
  - `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/cch-algorithm-2175-verification.md` (re-verification result vs the existing `cch-algorithm.md`)
- Read-only reference (do not modify): `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/cch-algorithm.md`
- Optional local-only scratch (must not be committed): unpacked 2.1.175 npm wrapper / native binary workspace under a temp dir outside the repo, e.g. `/tmp/cc-2175-unpack/` (note: 2.1.175 ships a native binary via optionalDependencies, not a `cli.js` bundle)

- [ ] **Step 1.1: Add failing test if command argv prompt redaction is missing**

Inspect `_safe_command_display()` in `tools/claude_code_lab_capture.py`.

If it can persist prompt-like argv for `--print`, add a test similar to:

```python
def test_safe_command_display_redacts_print_prompt():
    got = _safe_command_display([
        "npx", "-y", "-p", "@anthropic-ai/claude-code@2.1.175",
        "claude", "--print", "hello secret prompt",
    ])
    assert "hello secret prompt" not in " ".join(got)
    assert "<redacted-test-prompt>" in got
```

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
python3 -m unittest tools.tests.test_claude_code_lab_capture
```

Expected: fails before implementation if the test was needed.

- [ ] **Step 1.2: Implement minimal safe command redaction if needed**

Update `_safe_command_display()` so it redacts argv values after `--print`, `-p` only when it is Claude prompt-like after the binary boundary, or other prompt-bearing CLI flags. Do **not** redact npm package `-p @anthropic-ai/claude-code@2.1.175`.

Run:

```bash
python3 -m unittest tools.tests.test_claude_code_lab_capture
```

Expected: pass.

- [ ] **Step 1.3: Run global and isolated version smoke**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
claude --version
npx -y -p @anthropic-ai/claude-code@2.1.175 claude --version
```

Expected:

```text
2.1.175 (Claude Code)
2.1.175 (Claude Code)
```

- [ ] **Step 1.4: Capture safe local shape using isolated command**

Use the existing wrapper. Do not add an extra `--` after the wrapper separator.

Example command shape, replacing only safe environment placeholders locally:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
export ZHUMENG_API_BASE='http://198.12.67.185:18080'
export ZHUMENG_CAPTURE_LEVEL='deep'
export ZHUMENG_NETWATCH_INTERVAL='2.0'
# export ZHUMENG_API_KEY='...local test key, do not paste into logs...'

tools/claude-code-lab-start.sh npx -y -p @anthropic-ai/claude-code@2.1.175 claude
```

Inside Claude Code, run only minimal safe prompts needed to trigger:

- `/v1/messages`
- `/v1/messages/count_tokens` if naturally triggered or available
- at least one tool/classifier-related safe read-only flow if needed
- model requests for `claude-opus-4-8` and `claude-fable-5` only if the account entitlement permits; otherwise record entitlement failure as non-blocking for persona shape.

Do not store raw prompts or raw bodies.

- [ ] **Step 1.5: Produce safe evidence deliverable**

Keep raw/local capture output and the committed safe deliverable separate. Raw/local artifacts may remain under the lab run directory for local debugging, but only files under `safe-deliverable/` may be considered for git. Create the safe deliverable directory and include only sanitized facts:

```json
{
  "target_cli_version": "2.1.175",
  "observed_user_agent_shape": "claude-cli/2.1.175 (external, sdk-cli)",
  "observed_stainless_package_version": "...",
  "observed_stainless_runtime_version": "...",
  "messages_beta_tokens": ["..."],
  "count_tokens_beta_tokens": ["..."],
  "billing_header_version_prefix": "2.1.175",
  "stores_raw_prompt": false,
  "stores_raw_token": false,
  "notes": ["sanitized summary only"]
}
```

Do not include raw payloads, raw prompt text, raw user metadata, raw API keys, raw emails, or raw account IDs.

- [ ] **Step 1.5a: Inspect the 2.1.175 npm package layout (native binary reality)**

IMPORTANT (re-verify for 2.1.175 on 2026-06-12): the `@anthropic-ai/claude-code@2.1.175` npm package is NOT a ripgrep-able `cli.js` bundle. It is a thin wrapper:

- `package/install.cjs`, `package/cli-wrapper.cjs`, `package/bin/claude.exe` (placeholder), `package/sdk-tools.d.ts`, `package/package.json`.
- The real CLI is a platform-native compiled binary delivered via `optionalDependencies`, e.g. `@anthropic-ai/claude-code-darwin-arm64@2.1.175` (a single ~223MB native `claude` executable, not JavaScript).

Therefore source-level constant extraction by `rg cli.js` does NOT apply to 2.1.175. Confirm the layout locally first (read-only, in a temp dir outside both repos):

```bash
UNPACK=$(mktemp -d /tmp/cc-2175-unpack.XXXXXX) && cd "$UNPACK"
npm pack @anthropic-ai/claude-code@2.1.175
tar -xzf anthropic-ai-claude-code-2.1.175.tgz
find package -type f
cat package/package.json   # confirms optionalDependencies native packages + version
```

Decision for reverse engineering:

- Static reverse engineering now means analyzing a large stripped native binary (`strings`/disassembly), which is heavy and low-yield. Do NOT block the upgrade on it.
- Treat Step 1.5b (runtime re-verification against real 2.1.175 traffic) as the PRIMARY, authoritative evidence for whether the CCH / cc_version algorithm changed.
- Only escalate to native-binary static analysis if Step 1.5b shows a mismatch that runtime evidence alone cannot explain. If so, stop and surface to the user before spending effort on binary RE.

Do NOT commit the unpacked wrapper, the native binary, `strings` output, or any extracted constant into git. Keep everything under `/tmp` scratch.

- [ ] **Step 1.5b: Re-verify CCH + cc_version algorithm against real 2.1.175 (PRIMARY authoritative evidence)**

This step decides whether the signing algorithm changed in 2.1.175. It must be reproducible, so be precise about inputs.

Synthetic prompt requirements (so the test actually exercises the algorithm):

- Author 2+ harmless, self-owned, non-sensitive ASCII prompts.
- Each prompt MUST be at least 21 characters long and have **distinct** characters at UTF-16LE indices 4, 7, and 20, so the `cc_version` suffix genuinely depends on `CC_VERSION_POSITIONS = [4,7,20]` (otherwise a short/uniform prompt yields `'0'` filler and proves nothing).
- Example shape (verify indices, do not just copy): `"abcdEfgHijklmnopqrstUvwxyz-check"` — confirm chars at 4/7/20 differ before using.

Capture requirements (CCH is over the FULL body, not just the header):

- Capture one real 2.1.175 signed `/v1/messages` request for each synthetic prompt. CCH is computed over the **complete original JSON request body bytes** with the signed `cch=<5hex>;` normalized back to `cch=00000;` — recomputing from the billing header text alone is INVALID.
- The raw request body is used only transiently in local `/tmp` scratch or in memory. Do NOT save the raw body, do NOT commit it, do NOT paste it into docs or chat. The safe deliverable records only yes/no comparisons.

Recompute offline using the authoritative constants:

- Reference algorithm doc (read-only): `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-antiban-implementation/docs/anti-ban/cch-algorithm.md`.
- CCH: `xxh64(full_body_with_cch=00000_placeholder, SEED=0x4d659218e32a3268) & 0xFFFFF`, formatted as 5 lower-hex.
- cc_version 3-hex suffix: `sha256( "59cf53e54c78" + chars + "2.1.175" )[:3]`, where `chars` are UTF-16LE indices `[4,7,20]` of the first non-`<system-reminder>` user text.

Easiest way to reuse the authoritative implementation is the CC Gateway exported functions (note: `computeCCH5Hex` is NOT exported; use the exported `verifySignedCCH` and `computeCCVersionSuffix`). Write a tiny throwaway `tsx` scratch in `/tmp` (not in the repo):

```bash
# raw body saved transiently to a /tmp scratch file only; delete after.
RAW=/tmp/cc-2175-body.json   # local scratch, never committed
cat > /tmp/verify-2175.mjs <<'JS'
import { readFileSync } from 'fs'
import { verifySignedCCH, computeCCVersionSuffix } from '/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts'
const body = readFileSync(process.env.RAW)            // full signed body bytes
const v = verifySignedCCH(Buffer.from(body))          // re-normalizes cch=00000 then recomputes
console.log('cch_verify_ok', v.ok)
// also recompute cc_version suffix from the first user text + "2.1.175"
JS
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
RAW="$RAW" npx tsx /tmp/verify-2175.mjs
```

If `verifySignedCCH` returns `ok:true` for the real 2.1.175 body, the CCH seed/mask/placeholder convention is unchanged. Separately confirm the observed `cc_version` suffix equals `computeCCVersionSuffix(firstUserText, "2.1.175")`.

Record in `cch-algorithm-2175-verification.md` (sanitized, yes/no only):

- recomputed CCH == observed CCH (full-body verification): yes/no.
- recomputed cc_version suffix == observed suffix, with a prompt that exercises positions 4/7/20: yes/no.
- conclusion: algorithm UNCHANGED for 2.1.175, or CHANGED (state which component).

Clean up `/tmp` scratch (raw body + scratch scripts) after recording the yes/no result.

- [ ] **Step 1.5c: Algorithm-change decision gate**

Decide scope based on Step 1.5a/1.5b results:

- If CCH seed/mask, cc_version salt/positions/length, hash functions, and billing header shape are all UNCHANGED: keep this plan's scope as a persona/version-string upgrade. Proceed to Step 1.6.
- If ANY signing component CHANGED in 2.1.175: STOP and escalate. This becomes a signing-constant change, not just a version bump. The authoritative CC Gateway signer (`src/policy.ts` in the implementation worktree) must be updated (and any active Sub2API signer path reconciled). Capture the corrected algorithm in a NEW doc inside the current implementation worktree (do NOT edit the read-only reference at `.worktrees/claude-antiban-implementation/docs/anti-ban/cch-algorithm.md`), and the change must go back through review before implementation continues. Surface this to the user explicitly before proceeding.

- [ ] **Step 1.6: Diff 2.1.175 evidence against current 2.1.150 assumptions**

Compare evidence against:

- `backend/internal/pkg/claude/constants.go`
- `backend/internal/service/cc_gateway_adapter.go`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-registry.ts`
- `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts`

Record in safe deliverable:

- Whether UA version changes only, or Stainless/runtime/beta sequence also changes.
- Whether `anthropic-beta` ordering changed.
- Whether `cc_version` suffix format remains three hex characters.
- Whether `cch=00000` remains expected in locally signed/fake contexts or if real CLI evidence differs.
- The `x-stainless-*` header set and values observed for 2.1.175 (package-version, runtime, runtime-version, os, arch, lang).
- Cross-reference Step 1.5a/1.5b: confirm whether the CCH and cc_version signing algorithm/constants are unchanged for 2.1.175.

- [ ] **Step 1.7: Run capture/tooling tests**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
python3 -m unittest \
  tools.tests.test_claude_code_lab_capture \
  tools.tests.test_cli_control_plane_guard \
  tools.tests.test_cli_control_plane_policy
```

If `tools/tests/test_claude_code_lab_capture.py` was not created, omit `tools.tests.test_claude_code_lab_capture` from this command.

Expected: pass.

- [ ] **Step 1.8: Commit checkpoint 1 if files changed**

Only stage exact changed paths, for example:

```bash
# Non-docs paths: normal git add.
git add tools/claude_code_lab_capture.py \
  tools/tests/test_claude_code_lab_capture.py
# docs/* is gitignored, so the safe-deliverable evidence MUST be force-added:
git add -f \
  docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/README.md \
  docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/evidence.json \
  docs/anti-ban/captures/real-baseline/2026-06-11-claude-code-2175-local-capture/safe-deliverable/cch-algorithm-2175-verification.md
# Omit tools/tests/test_claude_code_lab_capture.py from git add if no new test file was created.
# cch-algorithm-2175-verification.md is required evidence for later review/docs/acceptance; do not omit it.
git commit -m "docs: capture claude code 2.1.175 persona evidence"
```

If only local untracked non-deliverable raw capture artifacts exist, do not commit them.

- [ ] **Step 1.9: Checkpoint 1 review**

Reviewer verifies:

- CCH and cc_version signing algorithm/constants were re-verified against real 2.1.175 (Step 1.5b), and the unpack/constant check (Step 1.5a) result is recorded.
- The algorithm-change decision gate (Step 1.5c) was applied and its outcome (unchanged vs escalate) is explicit.
- `x-stainless-*` fingerprint set for 2.1.175 is captured.
- No unpacked wrapper, native binary, `strings`/disassembly output, raw source, raw request body, or newly extracted constant value was committed.

- Evidence is sufficient to decide Sub2API/CC Gateway profile changes.
- No raw secrets/payloads/prompts are committed.
- Capture command did not globally upgrade Claude Code.
- Existing fake-client blocking assumptions were not weakened.

---

## Checkpoint 2: Upgrade Sub2API verified persona to 2.1.175

**Purpose:** Make Sub2API normal production Claude Code-compatible outbound persona canonicalize to verified 2.1.175.

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend/internal/pkg/claude/constants.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend/internal/pkg/claude/constants_test.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend/internal/service/cc_gateway_adapter.go`
- Modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend/internal/service/cc_gateway_adapter_test.go`
- Modify as required by failing tests:
  - `backend/internal/service/identity_service.go`
  - `backend/internal/service/identity_service_defaults_test.go`
  - `backend/internal/service/formal_pool_onboarding_service_test.go`
  - `backend/internal/service/local_capture_acceptance_artifact_test.go`
  - `backend/internal/service/gateway_cc_gateway_boundary_test.go`
  - `backend/internal/service/claude_code_native_shape_healthcheck_test.go`
  - `backend/internal/handler/gateway_native_attestation_test.go`
  - `backend/internal/handler/gateway_helper_hotpath_test.go`

- [ ] **Step 2.1: Write failing Sub2API default persona tests**

In `backend/internal/pkg/claude/constants_test.go`, rename the test to `TestClaudeCodePersonaDefaultsTrack2175` and assert:

```go
require.Equal(t, "2.1.175", CLICurrentVersion)
require.Equal(t, "claude-cli/2.1.175 (external, sdk-cli)", DefaultHeaders["User-Agent"])
```

If Checkpoint 1 evidence changed Stainless/runtime versions, update those expectations too. If evidence did not prove a change, keep existing values.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend
go test ./internal/pkg/claude -run 'PersonaDefaults|EndpointSpecificBetas|DefaultModels' -count=1
```

Expected: fail before implementation.

- [ ] **Step 2.2: Write failing CC Gateway policy canonicalization tests**

In `backend/internal/service/cc_gateway_adapter_test.go`, add/adjust tests covering:

1. `ccGatewayAnthropicPolicyVersion == "2.1.175"`.
2. Normal production account with `Extra["cc_gateway_policy_version"] = "2.1.150"` still emits final `x-cc-policy-version: 2.1.175` after passing compatibility/admission.
3. Explicit trusted inbound context using `SetClaudeCodeVersion(ctx, "2.1.175")` emits `2.1.175`.
4. Fake or incompatible version, e.g. `"2.1.126.test"`, remains incompatible.
5. Same-minor compatibility decision is explicit: either allow `2.1.150` as stale account metadata for migration but canonicalize final emission, or reject stale account metadata and provide a migration path. Prefer allow-and-canonicalize to avoid damaging production account data.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend
go test ./internal/service -run 'CCGateway|PolicyVersion|AnthropicPolicy|Stale|FormalPoolOnboarding' -count=1
```

Expected: fail before implementation.

- [ ] **Step 2.3: Update Claude constants from evidence**

In `backend/internal/pkg/claude/constants.go`:

- Set `CLICurrentVersion = "2.1.175"`.
- Update comments that specifically say observed `2.1.150` to `2.1.175` only where evidence confirms the profile.
- If Checkpoint 1 evidence shows beta order/additions/removals changed, update:
  - `ClaudeCodeMessagesBetas()`
  - `ClaudeCodeMessagesOAuthBetas()`
  - `ClaudeCodeMessagesOAuthBetasForBody()`
  - `ClaudeCodeCountTokensOAuthBetas()`
  - `FullClaudeCodeMimicryBetas()`
- If evidence does not show beta changes, keep token sets stable and only update version comments.

Run:

```bash
gofmt -w backend/internal/pkg/claude/constants.go backend/internal/pkg/claude/constants_test.go
go test ./internal/pkg/claude -count=1
```

Expected: pass.

- [ ] **Step 2.4: Update Sub2API CC Gateway policy emission**

In `backend/internal/service/cc_gateway_adapter.go`:

- Set `ccGatewayAnthropicPolicyVersion = "2.1.175"`.
- Introduce a helper such as:

```go
func canonicalCCGatewayAnthropicPolicyVersion() string {
    return ccGatewayAnthropicPolicyVersion
}
```

or a more useful resolver such as:

```go
func resolveCCGatewayOutboundPolicyVersion(ctx context.Context, account *Account) string {
    if ccGatewayTrustedPersonaContext(ctx) {
        if version := strings.TrimSpace(GetClaudeCodeVersion(ctx)); ccGatewayPolicyVersionCompatible(version) {
            return ccGatewayAnthropicPolicyVersion
        }
    }
    return ccGatewayAnthropicPolicyVersion
}
```

The exact implementation can differ, but normal final outbound `x-cc-policy-version` must be `2.1.175` after upgrade.

- Keep `isCCGatewayEligibleAccount` safe for production data:
  - If account Extra is empty/mismatch today, do not silently route it unless existing logic already allows it.
  - If account Extra is `2.1.150`, allow migration compatibility and canonicalize final header to `2.1.175`; leave the DB Extra value intact as diagnostic/legacy metadata unless a separate migration is explicitly approved.
  - Reject malformed values like `2.1.126.test` and unrelated major/minor versions.
- Update comments from first-wave 2.1.150 to verified 2.1.175.

Run:

```bash
gofmt -w backend/internal/service/cc_gateway_adapter.go backend/internal/service/cc_gateway_adapter_test.go
go test ./internal/service -run 'CCGateway|PolicyVersion|AnthropicPolicy|FormalPoolOnboarding' -count=1 -timeout=240s
```

Expected: pass.

- [ ] **Step 2.5: Update default identity/formal-pool/version fixtures**

Fix failing tests and comments that refer to the canonical production persona:

- `backend/internal/service/identity_service.go`
- `backend/internal/service/identity_service_defaults_test.go`
- `backend/internal/service/formal_pool_onboarding_service_test.go`
- `backend/internal/service/local_capture_acceptance_artifact_test.go`
- `backend/internal/service/gateway_cc_gateway_boundary_test.go`
- `backend/internal/service/claude_code_native_shape_healthcheck_test.go`
- `backend/internal/handler/gateway_native_attestation_test.go`
- `backend/internal/handler/gateway_helper_hotpath_test.go`

Guideline:

- Tests for canonical current persona should move to 2.1.175.
- Tests for legacy/downgrade/spoof scenarios may keep 2.1.146 or 2.1.150 intentionally, but rename/comments must make that intent explicit.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend
go test ./internal/service ./internal/handler -run 'ClaudeCode|CCGateway|PolicyVersion|NativeShape|Persona|Identity|FormalPool|LocalCapture|Hotpath' -count=1 -timeout=300s
```

Expected: pass.

- [ ] **Step 2.6: Verify spoof/fake protections still pass**

Run targeted tests:

```bash
go test ./internal/service ./internal/handler ./internal/server/routes -run 'Suspicious|Spoof|Probe|Detection|Validator|Protocol|Thinking|BadThinking|Boundary|Compat' -count=1 -timeout=300s
```

Expected: pass. If a test is missing for `.test` or all-zero metadata blocking, add one before implementing fixes.

- [ ] **Step 2.7: Run broader backend checks**

Run:

```bash
go test ./internal/pkg/claude ./internal/service ./internal/handler ./internal/server/routes -count=1 -timeout=300s
go build ./cmd/server
```

Expected: pass.

- [ ] **Step 2.8: Commit checkpoint 2**

Stage exact files only, for example:

```bash
git add \
  backend/internal/pkg/claude/constants.go \
  backend/internal/pkg/claude/constants_test.go \
  backend/internal/service/cc_gateway_adapter.go \
  backend/internal/service/cc_gateway_adapter_test.go \
  backend/internal/service/identity_service.go \
  backend/internal/service/identity_service_defaults_test.go \
  backend/internal/service/formal_pool_onboarding_service_test.go \
  backend/internal/service/local_capture_acceptance_artifact_test.go \
  backend/internal/service/gateway_cc_gateway_boundary_test.go \
  backend/internal/service/claude_code_native_shape_healthcheck_test.go \
  backend/internal/handler/gateway_native_attestation_test.go \
  backend/internal/handler/gateway_helper_hotpath_test.go
git commit -m "feat: upgrade sub2api claude code persona to 2.1.175"
```

Adjust exact list to actual changed files.

- [ ] **Step 2.9: Checkpoint 2 review**

Reviewer verifies:

- Final Sub2API normal production persona emits 2.1.175.
- Stale account Extra 2.1.150 cannot leak into final normal outbound header.
- Fake-client blocking remains intact.
- No new model catalog/pricing regressions were introduced.
- Tests/build pass.

---

## Checkpoint 3: Upgrade standalone CC Gateway verified persona to 2.1.175

**Purpose:** Ensure Sub2API and standalone CC Gateway agree. Sub2API-only upgrade is incomplete.

**Repository/worktree:**

```text
/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
```

Do not edit `/Users/muqihang/chelingxi_workspace/cc-gateway` main working tree directly.

**Files:**

- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-registry.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/persona-resolver.ts`
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/policy.ts` if tests reveal hard-coded expectations.
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/src/proxy.ts` only if final forwarding still hard-codes 2.1.150.
- Modify: `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/config.example.yaml`
- Tests:
  - `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-registry.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/persona-resolver.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/session-and-beta-policy.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/checkpoint3-remediation.test.ts`
  - `/Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main/tests/config.test.ts`
  - Additional proxy/canary tests as failures indicate.

- [ ] **Step 3.1: Inspect standalone CC Gateway implementation worktree hygiene**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
git branch --show-current
git status --short
git log --oneline -n 8
```

Expected:

- Branch is `codex/claude-code-2173-main`.
- Worktree is clean.
- If tracked files are dirty, stop and ask before editing.

- [ ] **Step 3.2: Write failing persona registry/resolver tests**

Update or add tests asserting:

- `getPersonaProfile('claude_code_2_1_175_subscription_1m')` exists.
- Profile version is `2.1.175`.
- Profile known models include both `claude-opus-4-8` and `claude-fable-5`.
- Normal fallback resolves to `claude_code_2_1_175_subscription_1m`.
- Old `claude_code_2_1_150_subscription_1m` remains available only as explicit legacy profile.
- Same-minor drift behavior is explicit and safe.

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/persona-registry.test.ts
npx tsx tests/persona-resolver.test.ts
```

or the repo's actual test command from `package.json`.

Expected: fail before implementation.

- [ ] **Step 3.3: Implement 2.1.175 profile in persona registry**

In `src/persona-registry.ts`:

- Add `claude_code_2_1_175_subscription_1m` using Checkpoint 1 evidence for beta/profile fields.
- Set aliases such as `claude-code-2.1.175-macos-local` if existing alias style uses that form.
- Include `claude-opus-4-8` and `claude-fable-5` in `knownModels`.
- Keep 2.1.150 profiles for explicit legacy tests/rollback.
- Update `inferLegacyPersonaVariant` so normal 2.1.175 input resolves to the new profile and normal fallback no longer selects 2.1.150.

- [ ] **Step 3.4: Update resolver/policy final emission**

In `src/persona-resolver.ts`:

- Make `fallbackProfile()` return `claude_code_2_1_175_subscription_1m`.
- Ensure stale incoming `2.1.150` from trusted Sub2API migration either resolves to canonical 2.1.175 or is rejected with a clear diagnostic according to the Sub2API decision. Prefer canonicalize for normal production.
- Ensure untrusted inbound headers cannot self-promote to 2.1.175.

In `src/policy.ts` / `src/proxy.ts` only as needed:

- Verify `User-Agent` is `claude-cli/2.1.175 (external, sdk-cli)` for normal shared-pool Anthropic requests.
- Verify signed billing header includes `cc_version=2.1.175.<3hex>`.
- Verify verifier expected version/beta align with resolver decision.

- [ ] **Step 3.5: Update config example and tests**

In `config.example.yaml`:

- `env.version: "2.1.175"`
- `env.version_base: "2.1.175"`
- default `message_beta_profile: claude_code_2_1_175_subscription_1m`
- account identity `persona_variant: "claude-code-2.1.175-macos-local"`
- account identity `policy_version: "2.1.175"`
- include `claude-fable-5` in candidate allowlist/fixtures only if that config section models candidate new models.

Update tests to match.

- [ ] **Step 3.6: Run standalone CC Gateway tests**

Run exact package command from `package.json`. Likely:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
npx tsx tests/persona-registry.test.ts
npx tsx tests/persona-resolver.test.ts
npx tsx tests/session-and-beta-policy.test.ts
npx tsx tests/checkpoint3-remediation.test.ts
npx tsx tests/config.test.ts
npm test
npm run build
```

Expected: pass.

- [ ] **Step 3.7: Commit checkpoint 3 in CC Gateway repo**

Stage exact files only:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
git add \
  src/persona-registry.ts \
  src/persona-resolver.ts \
  src/policy.ts \
  src/proxy.ts \
  config.example.yaml \
  tests/persona-registry.test.ts \
  tests/persona-resolver.test.ts \
  tests/session-and-beta-policy.test.ts \
  tests/checkpoint3-remediation.test.ts \
  tests/config.test.ts
git commit -m "feat: add claude code 2.1.175 persona profile"
```

Adjust exact list to actual changed files. Do not stage `.claude/` or `.worktrees/`.

- [ ] **Step 3.8: Checkpoint 3 review**

Reviewer verifies:

- Standalone CC Gateway implementation was done in its fresh local-main worktree.
- Standalone CC Gateway default/fallback profile is 2.1.175.
- UA, beta, signed `cc_version`, and verifier expected version are aligned.
- Legacy 2.1.146/2.1.150 tests remain intentional.
- `claude-fable-5` is included where needed.
- Full tests/build pass.

---

## Checkpoint 4: Joint smoke, docs, and rollout readiness

**Purpose:** Validate Sub2API and CC Gateway together before production.

**Files:**

- Create or modify: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/docs/anti-ban/46-claude-code-2175-persona-upgrade.md`
- Modify if useful: `/Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/docs/codex-gateway/smoke.md`
- Potentially add smoke fixtures/tests in Sub2API or CC Gateway if no existing test covers final joint emission.

- [ ] **Step 4.1: Run Sub2API frontend regression checks for existing model selector**

Because local main already includes new models, only regression-check them:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/frontend
pnpm vitest run src/composables/useModelWhitelist.test.ts src/composables/__tests__/useModelWhitelist.spec.ts
pnpm vue-tsc --noEmit
pnpm build
```

Expected: pass.

- [ ] **Step 4.2: Run broad Sub2API backend tests/build**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main/backend
go test ./internal/pkg/claude ./internal/service ./internal/handler ./internal/server/routes -count=1 -timeout=300s
go build ./cmd/server
```

Expected: pass.

- [ ] **Step 4.3: Run joint local smoke**

Using local built artifacts or test harness, prove final CC Gateway-bound request has:

- `User-Agent: claude-cli/2.1.175 (external, sdk-cli)` after CC Gateway signing/resolution.
- `anthropic-beta` exactly matching Checkpoint 1 verified profile.
- billing text/body contains `cc_version=2.1.175.<3hex>`.
- Sub2API request to CC Gateway has `x-cc-policy-version: 2.1.175`.
- Stale account metadata `cc_gateway_policy_version=2.1.150` does not leak to final normal outbound persona.
- Fake `.test` / all-zero metadata probe is still blocked before upstream.

If an existing harness already does this, extend it rather than creating a new one.

- [ ] **Step 4.4: Write final docs**

Create `docs/anti-ban/46-claude-code-2175-persona-upgrade.md` with:

- Scope: CLI persona/profile upgrade only; new model catalog/pricing already existed on local main.
- CCH / cc_version algorithm re-verification result for 2.1.175 (unchanged vs changed), referencing the safe `cch-algorithm-2175-verification.md`.
- Captured `x-stainless-*` fingerprint set for 2.1.175.
- Evidence summary path, sanitized only.
- Exact changed constants/profiles in Sub2API and CC Gateway.
- Test command results.
- Rollback plan: revert Sub2API commit + CC Gateway commit or redeploy previous pinned artifacts; do not edit production account data as rollback.
- Canary plan:
  1. Deploy Sub2API artifact.
  2. Deploy CC Gateway artifact if production uses standalone CC Gateway.
  3. Run one low-token `claude-opus-4-8` request and one `claude-fable-5` request where entitlement permits.
  4. Confirm logs show 2.1.175 persona and no 2.1.150 final emission.

- [ ] **Step 4.5: Grep for unintended stale active anchors**

Run:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
rg -n "2\.1\.150|claude_code_2_1_150|claude-code-2\.1\.150" backend frontend tools docs -g '!docs/anti-ban/captures/**' -g '!node_modules' -g '!dist' -g '!build'

cd /Users/muqihang/chelingxi_workspace/cc-gateway/.worktrees/claude-code-2173-main
rg -n "2\.1\.150|claude_code_2_1_150|claude-code-2\.1\.150" src tests config.example.yaml -g '!node_modules' -g '!dist' -g '!build'
```

Expected:

- Remaining 2.1.150 references are explicitly legacy/rollback/spoof/regression tests or historical docs.
- No default/fallback/current/canonical production path remains 2.1.150.

- [ ] **Step 4.6: Commit checkpoint 4 docs/smoke changes**

Stage exact files only, for example:

```bash
cd /Users/muqihang/chelingxi_workspace/sub2api-zhumeng-main/.worktrees/claude-code-2173-main
git add docs/anti-ban/46-claude-code-2175-persona-upgrade.md docs/codex-gateway/smoke.md
git commit -m "docs: record claude code 2.1.175 rollout evidence"
```

Adjust exact list to actual changes.

- [ ] **Step 4.7: Checkpoint 4 review**

Reviewer verifies:

- Joint Sub2API + CC Gateway emission is covered.
- Docs are acceptance evidence, not just design prose.
- No stale default 2.1.150 anchors remain.
- Production deployment steps are reversible and do not mutate account data.

---

## Checkpoint 5: Production deployment and canary

**Purpose:** Deploy only after all code reviews/tests pass and user approves deployment.

**Files:** production artifacts only; no repo changes expected unless deployment docs are updated after the fact.

- [ ] **Step 5.1: Ask for explicit production deploy approval**

Before touching production:

- Summarize Sub2API commit(s).
- Summarize CC Gateway commit(s).
- Summarize tests/builds passed.
- Confirm whether production uses standalone CC Gateway and where it is deployed.
- Confirm backup procedure.

- [ ] **Step 5.2: Backup production artifacts/data safely**

Use existing deployment runbook. At minimum:

- Backup current Sub2API binary/image/config.
- Backup current CC Gateway artifact/config if used.
- Do not modify production database account rows for this upgrade.
- Specifically preserve formal-pool account data and proxy bindings.

- [ ] **Step 5.3: Deploy pinned artifacts**

Deploy Sub2API and CC Gateway artifacts built from the reviewed commits. Do not deploy uncommitted local files.

- [ ] **Step 5.4: Health checks**

Run:

- Sub2API health endpoint.
- Admin UI loads.
- Account list/formal pool dashboards load.
- CC Gateway health endpoint if separate.

- [ ] **Step 5.5: Low-risk canary**

Run one or more low-token tests with a test key/account:

- `/v1/messages` with `claude-opus-4-8` if account entitlement permits.
- `/v1/messages` with `claude-fable-5` if account entitlement permits.
- Claude Code CLI compatibility command from a known client.
- Fake `.test`/all-zero metadata probe should be rejected locally without hitting upstream.

Confirm logs show:

- Sub2API to CC Gateway: `x-cc-policy-version=2.1.175`.
- CC Gateway final upstream: `User-Agent` version 2.1.175.
- signed billing `cc_version=2.1.175.<3hex>`.
- no normal final 2.1.150 emission.

- [ ] **Step 5.6: Rollback criteria**

Rollback if:

- Real Claude Code CLI traffic gets false 400/403 due to persona/protocol mismatch.
- CC Gateway signs mismatched UA/billing/beta versions.
- New models fail due to local mapping rather than upstream entitlement.
- Production account data or formal pool dashboards regress.

Rollback action:

- Redeploy previous pinned Sub2API artifact.
- Redeploy previous pinned CC Gateway artifact.
- Do not edit account data as rollback unless separately diagnosed and approved.

- [ ] **Step 5.7: Post-deploy note**

Record:

- Artifact versions/commit hashes.
- Deployment time.
- Canary result.
- Any entitlement failures from upstream as separate non-code issue.

---

## Final acceptance checklist

- [ ] Fresh implementation worktree was created from local `main`.
- [ ] Isolated `@anthropic-ai/claude-code@2.1.175` evidence was collected safely.
- [ ] CCH and cc_version signing algorithm/constants were re-verified against real 2.1.175 (unchanged, or escalated if changed).
- [ ] `x-stainless-*` fingerprint set for 2.1.175 was captured and recorded.
- [ ] No unpacked wrapper, native binary, `strings` output, raw source, or newly extracted constant value was committed to git.
- [ ] No raw request body or raw prompt content was committed to git or written into docs.
- [ ] Sub2API `CLICurrentVersion` and default UA are 2.1.175.
- [ ] Sub2API normal CC Gateway policy version emission is 2.1.175.
- [ ] Stale account Extra 2.1.150 cannot leak to final normal production persona.
- [ ] Standalone CC Gateway has a verified 2.1.175 profile.
- [ ] Standalone CC Gateway fallback/default profile is 2.1.175.
- [ ] CC Gateway final UA, beta, signed `cc_version`, and verifier expected version align.
- [ ] `claude-opus-4-8` and `claude-fable-5` remain present in Sub2API and CC Gateway model surfaces.
- [ ] Spoof/fake old `.test` and all-zero metadata probes remain blocked.
- [ ] Backend tests/build pass.
- [ ] Frontend selector tests/vue-tsc/build pass.
- [ ] Standalone CC Gateway tests/build pass.
- [ ] Docs contain acceptance evidence and rollback plan.
- [ ] Production deployment is backed up and canaried before broader use.
