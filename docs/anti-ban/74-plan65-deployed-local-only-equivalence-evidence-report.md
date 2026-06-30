# 74 - Plan65 Deployed Local-only Equivalence Retry Evidence Report

Generated: 2026-06-30T17:55:06Z

## Final decision

**DEPLOYED_LOCAL_ONLY_EQUIVALENCE_READY**

This report is a Plan65 CP2/CP3 deployed local-only equivalence retry only. It is **not** live canary approval, **not** a deployed production TLS parity claim, and **not** production rollout approval. Any live canary or production enablement still requires separate explicit approval.

## Input anchors

- Plan70 SNI-preserving oracle report: `docs/anti-ban/70-sni-preserving-claude-code-tls-oracle-evidence-report.md`.
- Plan73 sidecar remediation report: `docs/anti-ban/73-cc-gateway-sidecar-tls-engine-remediation-report.md`.
- CC Gateway anchor commit: `b76c5a70631e7dc4d5f326562737b72f7208107a`.
- Sub2API anchor commit: `d06fb896281de070bf8ac6f9ce1ff6d78794a6a6`.
- Current CC Gateway HEAD at report time: `b76c5a70631e7dc4d5f326562737b72f7208107a`.
- Current Sub2API HEAD at report time: `d06fb896281de070bf8ac6f9ce1ff6d78794a6a6`.

Canonical retry oracle: Claude Code `2.1.179`, logical `api.anthropic.com` SNI/Host bucket `anthropic_api`, JA3 `d871d02cecbde59abbf8f4806134addf`, JA4 `t13d0017h1_18560269b2cb_92d925a272a4`, `cipher_count=17`, `extension_count=14`, ALPN `http/1.1`, TLS versions `0x0304,0x0303`, GREASE `false`.

## Evidence root

Safe evidence root: `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe`

Scratch artifacts are local-only under `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/scratch` and contain redacted overlay/safe summary only. No raw ClientHello, pcap, private key, certificate, raw body, raw prompt, raw response, or real secret is intentionally retained.

## Checkpoint results

| CP | Status | Evidence | Summary |
| --- | --- | --- | --- |
| CP0 anchor verification | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp0-anchor-verification.json` | Anchor commits and Plan70/Plan73 safe fields verified; old doc-63 no-SNI baseline not used. |
| CP1 local-only overlay + hash | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp1-config-overlay-hash.json` | Redacted local-only overlay created in scratch; no repo/production config mutation. |
| CP2 same-scope egress guard | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp2-egress-guard.json` | Loopback reachable; IPv4/IPv6/DNS/UDP/provider direct TCP/proxy-env paths blocked/rejected. |
| CP3 deployed local-only E2E | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp3-deployed-local-only-e2e.json` | Real Go/uTLS sidecar under sandboxed loopback-only same scope matched Plan70 oracle; Node direct fallback count `0`; real upstream request count `0`. |
| CP4 fail-closed / rollback | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp4-fail-closed-rollback.json` | Sidecar unavailable, disabled strict kill switch, summary mismatch, malformed/duplicate/conflicting summary, allowlist/profile mismatch, and forged hints fail closed without fallback. |
| CP5 leak scan | PASS | `/private/tmp/plan74-deployed-local-only-equivalence-20260630T170749Z/safe/cp5-leak-scan-summary.json` | Artifact-aware scan found no raw TLS, pcap, key/cert, raw body/prompt/response, real secrets, or proxy credentials. |

## TLS safe summary observed in CP3

```json
{
  "alpn_protocols": [
    "http/1.1"
  ],
  "cipher_count": 17,
  "extension_count": 14,
  "grease_present": false,
  "ja3_hash": "d871d02cecbde59abbf8f4806134addf",
  "ja4": "t13d0017h1_18560269b2cb_92d925a272a4",
  "mock_tls_trust_override": false,
  "profile_ref": "tls-profile:claude-code-2.1.179-real-oracle-tcp-v1",
  "raw_clienthello_omitted_reason": "raw_clienthello_forbidden",
  "sni_host_bucket": "anthropic_api",
  "sni_present": true,
  "source": "cc_gateway_utls_sidecar",
  "summary_bucket": "tls-bucket:claude-code-real-oracle-2179",
  "tls_versions": [
    "0x0304",
    "0x0303"
  ],
  "version": "tls-profile:claude-code-2.1.179-real-oracle-tcp-v1"
}
```

Decision fields:

- `matches_plan70_oracle`: `True`
- `same_scope_guarded_e2e`: `True`
- `same_scope_guard_type`: `container_loopback_only_sandbox_exec`
- `collector_clienthello_count`: `1`
- `node_direct_https_fallback_count`: `0`
- `real_upstream_request_count`: `0`

## Commands run

Targeted verification commands recorded for this retry:

```bash
cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5/sidecar/egress-tls-sidecar
go test ./...

cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5
TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 npx tsx tests/egress-tls-sidecar-real.test.ts
sandbox-exec -p '(version 1) (deny network*) (allow default) (allow network-outbound (remote tcp "localhost:*")) (allow network-inbound (local tcp "localhost:*"))' /bin/zsh -lc 'cd /Users/muqihang/chelingxi_workspace/cc-gateway-claude-platform-aws-cp5 && TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 node --import tsx tests/egress-tls-sidecar-real.test.ts'
TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 npx tsx tests/egress-tls-sidecar.test.ts
```

Final regression commands also completed with PASS before commit:

```bash
TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 npx tsx tests/egress-tls-profile.test.ts   # 5 passed, 0 failed
TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 npx tsx tests/proxy-sub2api.test.ts          # 43 passed, 0 failed
TMPDIR=/private/tmp npm_config_cache=/private/tmp/npm-cache-plan74 npx tsc --noEmit                            # PASS
```

## Implementation notes

- CC Gateway strict formal-pool TLS now fails closed when `shared_pool.egress_tls.enabled=true`, `strict=true`, and the TLS sidecar is disabled/unavailable; it does not proceed to Node direct HTTPS or the legacy proxy path.
- The real Go/uTLS sidecar command binds loopback only and requires an explicit loopback test dial override for local-only equivalence. The logical target remains `api.anthropic.com:443`; the override is sidecar-local test configuration, not request-controlled.
- The sidecar handler validates the authenticated control tuple and compares the actual in-memory ClientHello safe summary against the compiled expected profile before returning the safe summary bucket.
- No raw ClientHello is persisted; only safe summary fields are written to evidence.

## Production posture and next step

`DEPLOYED_LOCAL_ONLY_EQUIVALENCE_READY` means the **local-only deployed-equivalence retry gate is ready**: the Plan65 CP2/CP3 chain can be considered locally proven under loopback-only constraints with the remediated real Go/uTLS sidecar.

It does **not** mean production TLS parity has been proven against real deployed infrastructure. The next step, if desired, is to request a separate live canary/deployed equivalence approval with explicit scope, traffic limits, rollback controls, and no implicit Node direct fallback.
