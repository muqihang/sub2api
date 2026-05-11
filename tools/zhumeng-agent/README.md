# zhumeng-agent

Local managed setup tool for Codex integration in sub2api.

Current scope:

- scaffolded CLI entrypoints
- command parsing and dry-run placeholders
- future managed proxy / config repair / launcher integration

Attribution:

- Launcher / renderer injection structure is informed by the local CodexPlusPlus reference checkout in `temp/reference-repos/CodexPlusPlus`.
- Config / deeplink workflow ideas are informed by the local cc-switch reference checkout in `temp/reference-repos/cc-switch`.

Planned commands:

- `zhumeng-agent setup --client codex --code <code> --server <origin>`
- `zhumeng-agent launch codex --dry-run`
- `zhumeng-agent codex -- --version`
- `zhumeng-agent doctor --json`
- `zhumeng-agent repair codex`
- `zhumeng-agent logout --local-only`
- `zhumeng-agent logout --revoke-device`
