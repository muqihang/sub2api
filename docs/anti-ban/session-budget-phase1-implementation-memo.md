# Session Budget Phase 1 observe-only implementation memo

Date: 2026-05-25
Scope: Sub2API worktree `.worktrees/claude-antiban-implementation`

## Summary

Phase 1 implements safe observe-only ledgers for Session, Account, User, Pool Utilization, Risk Event, and Budget Decision. It is wired into `GatewayService` as an observe sink and does not mutate Claude Code request or response bodies.

## Safety boundaries

- No raw prompt, body, tool input/output, token, Authorization, CCH, email, account UUID, user UUID, or proxy credential is stored.
- Refs are internally generated scoped HMACs or validated safe account refs; decision summaries only expose internal HMAC refs.
- Free-text risk reasons are bucketed to `reason_*` codes.
- P0 safety events may recommend block/quarantine; budget pressure does not.

## Scheduling strategy

- `normal`: smooth 7-day 90%-100% target.
- `aggressive`: 3-day 95%-100% target with stronger catch-up.
- Both profiles only adjust scheduling weight/priority/cooldown recommendations.
- Cooldown, malformed headers, and P0 safety override catch-up.

## Implemented test coverage

- Rich Claude Code requests are observed without capability limits.
- Strict passthrough body bytes remain unchanged.
- CC Gateway boundary behavior is preserved.
- Missing/malformed/non-finite utilization headers are conservative observe.
- 429 produces cooldown recommendation.
- 403 risk text produces a risk event.
- normal/aggressive profiles do not bypass P0 or cooldown.
