# Zhumeng Agent Phase 1 Release Gate

## Required Evidence

- backend CodexAgent tests passed
- frontend one-click setup tests passed
- local manager pytest suite passed
- trusted-origin rejection tested
- setup code replay / single-use grant tested
- managed Codex config contains no raw API key
- loopback proxy / NO_PROXY behavior tested
- Chrome extension missing vs connected status tested
- Computer Use / Browser Use bundle detection tested
- injection failure must not block base Codex connectivity

## Phase 1 Scope Guard

- Codex App + Codex CLI only
- no Claude configuration writer / launcher in shipped Phase 1 path
- generic adapter scaffolding allowed
