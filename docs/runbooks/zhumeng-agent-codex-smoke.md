# Zhumeng Agent Codex Smoke

## Manual Acceptance

1. Create an API key in sub2api.
2. Open the key modal and click `一键接入 Codex`.
3. Install / open `逐梦 Agent`.
4. Confirm `~/.codex/config.toml` points to `127.0.0.1`.
5. Confirm `~/.codex/auth.json` does not contain a raw API key.
6. Run `zhumeng-agent doctor --json`.
7. Run `zhumeng-agent codex -- --version`.
8. Send one minimal Codex request.
9. Launch Codex App through `zhumeng-agent launch codex`.
10. Verify plugin status and delete-session entry.

## Automated Mocked Smoke

- mocked backend exchange / refresh
- managed state written locally
- Codex config uses managed loopback provider
- proxy injects managed headers
- 401/403 transition local state to `reauthorization_required`
- representative secret-bearing strings are redacted
