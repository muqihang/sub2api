# Claude Code 2.1.170 interim persona upgrade

Date: 2026-06-12

## Scope

This is an interim transition from the existing 2.1.150 persona to the highest available old-CCH-compatible Claude Code CLI version, 2.1.170. It is not a latest/final 2.1.175 persona upgrade.

Production deployment and real upstream canary remain out of scope until separately approved.

## CCH compatibility conclusion

Sanitized local raw-body capture/verifier investigation found:

| Version | cc_version suffix | fixed rotl64 CCH verifier | Conclusion |
| --- | --- | --- | --- |
| 2.1.150 | PASS | PASS | old-CCH compatible legacy |
| 2.1.153 | PASS | PASS | old-CCH compatible legacy |
| 2.1.169 | PASS | PASS | old-CCH compatible legacy |
| 2.1.170 | PASS | PASS | highest available old-CCH-compatible version |
| 2.1.171 | n/a | n/a | package version not published |
| 2.1.172+ | PASS | FAIL | CCH algorithm/preimage changed; separate delta investigation required |

Raw bodies, prompts, tokens, cookies, observed CCH values, and account identifiers are intentionally omitted. Runtime compatibility gates use this explicit verified corpus; unverified intermediate patches are not inferred as compatible.

## Model-marker evidence

Local package marker inspection found:

- `claude-opus-4-8` appears explicitly starting in Claude Code 2.1.154.
- `claude-fable-5` appears explicitly starting in Claude Code 2.1.170.

Local mock outbound-shape checks for 2.1.170 passed for both `claude-opus-4-8` and `claude-fable-5`, but this only proves request-shape formation. It does not prove real upstream account entitlement.

## Operational notes

- Existing production account metadata such as `cc_gateway_policy_version=2.1.150` must not be mutated by this change.
- Normal outbound CC Gateway policy is canonicalized to 2.1.170.
- 2.1.172+ and 2.1.175 remain blocked behind CCH delta investigation.
- Production canary requires a separately approved, real low-token request and log verification that final outbound persona is 2.1.170.
