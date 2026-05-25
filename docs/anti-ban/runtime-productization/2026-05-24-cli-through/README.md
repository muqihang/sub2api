# CLI-through runtime productization - 2026-05-24

This directory contains safe, token-free runtime artifacts generated from the verified successful Claude Code CLI-through canary shape.

Modes:

- `localhost-preflight`: localhost mock upstream only.
- `real-canary`: explicit one-off real canary; requires `ALLOW_REAL_ANTHROPIC_CANARY=1`, gateway token, egress proxy, and capture directory.
- `production-session`: production-capable runtime; requires `ALLOW_REAL_ANTHROPIC_PRODUCTION=1`, disables real-canary switches, and keeps Session Budget observe-only by default.
digest_omitted_by_policy: true

Safety properties encoded:

- `message_beta_profile=claude_code_2_1_150_subscription_1m`
- `env.version=2.1.150`, `env.version_base=2.1.150`
- `billing_cch_mode=sign`
- CCH signing enabled
- `context-1m-2025-08-07` enabled for Claude Code 1m-capable Sonnet/Opus context; current runtime validation fails closed if the 1m-enabled profile is not selected
- rich CLI capability envelope: `max_tokens=32000`, tools up to 40, thinking/context management enabled
- real-canary requires explicit capture directory and real-canary kill switch
- production-session requires the production switch and must not enable canary cost envelope or shared body caps

No raw tokens, Authorization values, raw prompts, raw body, raw CCH, account UUID, email, or proxy credentials are stored here.
## 2026-05-24 update: Claude Code 1m-capable Sonnet/Opus context

- Runtime productization now uses `claude_code_2_1_150_subscription_1m` so Claude Code 1m-capable Sonnet/Opus context is not disabled by our gateway profile.
- The previous `claude_code_2_1_150_subscription` profile remains available for historical canary comparison, but production-session artifacts require the 1m-enabled profile.
- The cost envelope continues to preserve rich Claude Code capability: max_tokens 32000, tools up to 40, thinking/context_management enabled.


## Known model envelope

- Current generated artifacts allow `claude-sonnet-4-6`, `claude-opus-4-7`, `claude-opus-4-7-thinking`, `claude-opus-4-6`, and `claude-opus-4-6-thinking`.
- Future Sonnet/Opus Claude Code models must go through dynamic model resolver / candidate_model_allowlist instead of being mechanically blocked or blindly trusted.
