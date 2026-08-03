# zhumeng-agent

Local managed setup tool for Codex integration in sub2api.

Current scope:

- scaffolded CLI entrypoints
- managed proxy / config repair / launcher integration
- Codex Desktop model picker patch commands
- Codex Desktop plugin auth gate patch commands
- Codex Desktop Capture V2 shape-only capture primitives

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
- `zhumeng-agent codex model-picker status --app /path/to/Codex.app`
- `zhumeng-agent codex model-picker patch --app /path/to/Codex.app`
- `zhumeng-agent codex model-picker restore --app /path/to/Codex.app`
- `zhumeng-agent codex plugin-auth-gate status --app /path/to/Codex.app`
- `zhumeng-agent codex plugin-auth-gate patch --app /path/to/Codex.app`
- `zhumeng-agent codex plugin-auth-gate restore --app /path/to/Codex.app`
- `zhumeng-agent codex capture status`
- `zhumeng-agent codex capture baseline --out <dir> [--app /path/to/Codex.app]`
- `zhumeng-agent codex capture install --app /path/to/Codex.app`
- `zhumeng-agent codex capture uninstall --app /path/to/Codex.app`
- `zhumeng-agent codex capture attach --cdp-port <port> --trace-dir <dir> [--once]`
- `zhumeng-agent codex capture report --trace-dir <dir> [--gateway-trace-dir <gateway-dir>]`

Desktop Capture V2 defaults to shape-only capture. It records protocol shape,
field names, lengths, hashes, content policy decisions, and correlation hashes,
but it does not persist raw prompts, source code, browser text, screenshots,
tool output, cookies, tokens, API keys, local absolute paths, repository URLs,
branch names, commit hashes, or raw Codex session/thread/turn/window IDs.

Raw payload capture is disabled unless the local diagnostic unlock is set:

```bash
ZHUMENG_CODEX_DESKTOP_CAPTURE_RAW_UNLOCK=I_UNDERSTAND_THIS_WRITES_LOCAL_RAW_DESKTOP_PROTOCOL_PAYLOADS
```

Capture install is observational and does not apply the model picker visibility
patch. Use the `codex model-picker` commands explicitly when the UI visibility
patch is needed.

The plugin auth gate patch is separate from the model picker patch. Current
Codex Desktop builds disable the Plugins navigation and Plugins page for
API-key auth. Use `codex plugin-auth-gate patch` when a managed Sub2API API-key
profile should be allowed to open the local Plugins marketplace UI. The patch
uses the same app.asar safety flow as the model picker patch: backup first,
unique patch point, integrity update, signing, status check, and restore.
Because the patch changes files on disk, any already-running Codex Desktop
window must be quit and reopened before the Plugins button reflects the change.
The patch and restore commands return `restart_required` and
`running_app_detected` fields to make this explicit.

For real Codex Desktop runs, `launch codex` starts a background
`codex capture attach` bridge when capture is installed. For manual smoke tests,
run `codex capture attach` after launching Desktop with a local
`--remote-debugging-port`. The attach command installs a `Runtime.addBinding`
bridge through the Chrome DevTools Protocol and is the primary event return path
for Desktop Capture. Renderer HTTP, beacon, and WebSocket delivery to
`127.0.0.1` are retained as fallback paths, but current Codex Desktop `app://`
renderer policy can block direct local network delivery.

When Gateway Capture output is stored separately from Desktop Capture output,
pass `--gateway-trace-dir` so the report command can write a unified
`trace_link.jsonl` in the Desktop trace directory.
