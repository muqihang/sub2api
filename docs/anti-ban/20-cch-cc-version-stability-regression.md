# 20 - CCH and cc_version Stability Regression

> **Design gate:** Docs 17-25 are second-layer shared-pool governance docs. They do not override `14-cc-gateway-shared-pool-compatibility-plan.md`. No implementation, production canary, or account onboarding may proceed from these docs until doc 14 P0-1..P0-6 have explicit pass/fail evidence. Core safety P0s (identity, egress, header allowlist, no double rewrite, joint capture, and no fallback) cannot be excluded; only route-scoped P0s may be excluded by disabling that route for first wave.
> **Parameter status:** Numeric caps, timings, scores, and thresholds in this document are calibration placeholders, not production defaults. First deployment must run them in observe-only mode, then promote with evidence.
> **Fail-closed rule:** If scheduler state, account identity, egress bucket, lifecycle stage, tombstone, or policy version is missing or inconsistent, the request must stop before CC Gateway. Do not silently fall back to native Sub2API mimicry or direct upstream.

> **Status:** Design draft. Not yet implemented.
> **Scope:** How we keep the CCH (5-hex body signature) and cc_version (3-hex first-user-message fingerprint) algorithms verified across CLI versions and time.
> **Companion docs:** `15-cch-algorithm-validation-and-usage-plan.md`, `16-no-cch-upstream-acceptance-validation.md`, `25-claude-code-2146-reverse-coverage-and-signing-readiness-gates.md`.
> **Risk posture:** Existing offline verification is on a small, correlated sample (8 fixtures, single CLI version). Production reliance must be conditional on continuous regression.

---

## 1. Why this is needed

Doc 15 confirmed the 5-hex CCH formula `xxh64(body_with_cch_00000, 0x4d659218e32a3268) & 0xFFFFF` against 8 Claude Code 2.1.145 raw localhost requests.

This is **strong initial evidence**, but:

- The 8 fixtures are from one machine, one session, consecutive captures (highly correlated).
- 2.1.146 has not yet been verified for CCH.
- The 3-hex `cc_version` suffix algorithm has not been independently verified.
- Anthropic may rotate the seed silently between minor versions.

Without continuous regression we cannot safely promote CCH signing from offline verifier to any manually approved opt-in signing mode.

---

## 2. Two algorithms, separately validated

### 2.1 CCH (5-hex)

```text
final_body = JSON bytes after all rewrites, with the billing field literally cch=00000;
cch = lower_hex_5( xxh64(final_body, 0x4d659218e32a3268) & 0xFFFFF )
```

### 2.2 cc_version suffix (3-hex)

```text
first_user_text = selected first user text used by Claude Code for billing attribution
chars = char_at(first_user_text, 4) + char_at(first_user_text, 7) + char_at(first_user_text, 20)
chars uses "0" for any missing position
suffix = sha256("59cf53e54c78" + chars + cli_version)[:3]
cc_version = "{cli_version}.{suffix}"
```

Notes:
- This is the 3-hex `cc_version` fingerprint, not the 5-hex body CCH. The current CC Gateway helper named `computeCCH()` actually implements this suffix-style helper and should be renamed before signing work.
- The exact text-selection and character-indexing rule matters. Current evidence points to Claude Code/JS-style indexing at positions `[4, 7, 20]`, with missing characters replaced by `0`, but 2.1.146 fixtures must verify this.
- The current Sub2API `extractFirstUserText()` may not handle `<system-reminder>` / first text block selection the same way as Claude Code. This is a known gap.
- Both algorithms must be re-checked when CLI updates.

---

## 3. Fixture sources

We must collect fixtures from independent capture sessions to break correlation:

| Source | What | When |
|---|---|---|
| S1: existing Sub2API local raw captures (2.1.145) | 8 reqs | Already captured |
| S2: new local raw captures (2.1.146, fresh machine session) | >= 8 reqs | Required before V2 |
| S3: cross-session captures (different days, different prompt themes) | >= 8 reqs | Required for stability claim |
| S4: minimal real-upstream fixtures (debug-mode capture) | small N | Optional, only if user explicitly approves |

Strict: S1+S2 alone is not "verified across versions". We need at least S3 to claim stability.

---

## 4. Independent re-implementation cross-check

Avoid bias by re-implementing the algorithm from at least two independent sources:

| Reference | Independent? | Used for |
|---|---|---|
| User-supplied `cch-algorithm.md` | Source spec | Primary |
| NTT123 `reconstruct_claude_code_billing_header.js` (2026-04 gist) | Independent | Cross-check (note: appears SHA-256 based, divergent) |
| Simpolism `claude-code-billing.js` (2026-04 blog) | Independent | Cross-check |
| `freshcrate/gproxy` rewrite logic | Independent | Cross-check |

If three independent re-implementations all match the same 8/8 fixtures, confidence is high. If they diverge, we likely captured a transient or per-CLI-version variant.

---

## 5. Regression cadence

| Trigger | Required action |
|---|---|
| New Claude Code patch version released | Run S2 capture; expect 100% CCH match |
| Failed real-upstream canary | Re-run S2 + S3; check if seed rotated |
| 30 days without CLI update | Run S3 sanity check anyway |
| Sub2API or CC Gateway billing-related code change | Re-run all stored fixtures |

Outcome of each regression run:
- All match: status remains `verified-version-X`.
- Any miss: mark `verified-version-X-broken`, freeze CCH signing as runtime feature, fall back to strip.

---

## 6. Toolchain

To make regression cheap:

1. A single Go (or Python) verifier script that:
   - Loads raw captures from a directory.
   - Detects billing block.
   - Recomputes CCH and cc_version with current seed/algorithm.
   - Prints per-request match status and aggregate hit rate.
2. Optional: store fixture summaries (hashes + expected CCH) in repo so historical fixtures can be re-validated.
3. CI hook: when relevant code changes, run the verifier on stored fixture summaries.

Existing `verify_cch_algorithm.go` (used in 2026-05-21 verification) is the starting point.

---

## 7. Production policy

Until S2+S3 both pass:

- CCH signer remains **offline verifier only**.
- Default shared-pool policy is **strip billing/CCH** (per doc 16 Phase A/B PASS).
- Do not enable CC Gateway server-side signing as default.
- Do not run a shared-pool signing canary while any doc 25 MUST reverse-coverage gate G1-G5 is still open.

After S2+S3 pass:

- CCH signer may be promoted only to **manually approved opt-in signing mode**, gated by config flag, applied per-account or per-route.
- Even after promotion, default remains strip until count_tokens / event_logging fixtures are also covered. Signing is not a fallback for strip failure; if strip is not verified healthy, the route/cohort halts unless opt-in signing has independently passed all gates.
- Promotion also requires doc 25 Sub2API route-boundary gates and CC Gateway final-output pipeline gates.

### 7.1 Runtime decision tree

The runtime path must be explicit and fail-closed:

```text
if route is not CC Gateway shared-pool route:
  use that route's own policy; do not borrow this signer

if shared-pool policy says strip:
  strip billing block and CCH
  verify final body/header has no billing block and no cch
  if verification fails: fail closed

if shared-pool policy says sign:
  normalize final body exactly once
  compute cc_version if required
  set billing block with cch=00000 placeholder
  serialize final body bytes
  compute CCH as last body mutation
  verify computed value against final body bytes
  if verification fails: fail closed

if neither strip nor sign is available:
  fail closed
```

There is no permitted fallback to:

- user-supplied CCH;
- user-supplied CCH/header passthrough;
- Sub2API native mimicry after CC Gateway has already rewritten;
- silent native fallback;
- unsigned billing block;
- direct upstream bypass;
- direct Anthropic upstream without CC Gateway;
- old cached body signed under a different final body.

---

## 8. Failure handling

If runtime CCH signing is enabled and signing fails:

- Fail closed: do not send the request unsigned.
- Log redacted error.
- Mark account as needing manual review.

If runtime signing succeeds but upstream returns 4xx that suggests body integrity failure:

- Disable CCH signing for that account immediately.
- Switch to strip only if the strip path is currently verified healthy for the same route, persona, and CLI version.
- If strip is not verified healthy, fail closed for that route/cohort.
- Open an investigation ticket: seed rotation or body canonicalization drift suspected.

If seed drift and strip-broken happen together, the only safe action is to halt the affected cohort. Do not continue traffic through native fallback.

---

## 9. Verification matrix

These are executable test specs:

| Case | Fixture / test hook | Expected |
|---|---|---|
| 8 stored 2.1.145 fixtures recompute | stored fixture dir | 8/8 match seed `0x4d659218e32a3268` |
| 8 fresh 2.1.146 fixtures recompute | fresh local capture fixture dir | 8/8 match same seed or explicit new seed record |
| `extractFirstUserText` skips `<system-reminder>` | cc_version fixture with reminder prefix | cc_version suffix matches |
| Three independent reimplementations on same fixture | verifier adapters | All match, or signing remains offline-only |
| Seed silently rotated upstream | mismatched fixture | Verifier reports miss; production never promotes signing |
| Strip verification fails | final body still contains billing/CCH | Request fails closed |
| Signing fails and strip not verified | runtime route fixture | Request fails closed; no native fallback |
| Signing succeeds but body mutates afterward | post-sign mutation fixture | Self-check fails before upstream |

---

## 10. Open questions

- Is the seed `0x4d659218e32a3268` observable in the leaked source map directly, or only inferred?
- Are there fields beyond `cch=00000` placeholder that the official CLI also normalizes pre-hash? If so, we need more body-shape rules.
- Does CCH cover only the JSON body, or also any header bytes? Current evidence suggests body-only, but no proof.
