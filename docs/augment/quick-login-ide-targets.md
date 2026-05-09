# Augment Quick Login IDE Targets

Status snapshot as of 2026-05-09 for backend quick-login deeplink generation.

## Rules

- Backend is the source of truth for launch eligibility.
- `editor_target` only affects `official_passthrough`.
- `local_compat` ignores `editor_target` and continues using the default `vscode` deeplink behavior.
- Unverified targets stay visible, but they are not launch-eligible by default.
- This iteration supports env-only scheme overrides. There is no config-file override path.

## Current Registry Matrix

| Target | Default scheme | Scheme verified | Handler verified | Enabled by default | Installed app assumption | Override required | Override env | Fallback instructions |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `vscode` | `vscode` | yes | yes | yes | VS Code with Augment base installed | no | none | Auto-launch is allowed. If launch fails unexpectedly, copy the deeplink and open it manually in the browser address bar. |
| `cursor` | `cursor` | yes | no | no | Cursor.app installed locally, Augment base compatibility not yet re-verified in this repo context | yes | `AUGMENT_IDE_SCHEME_CURSOR` | Do not auto-launch by default. Use copy-link/manual-open unless operator has explicitly opted in via env override. |
| `kiro` | `kiro` | no | no | no | No local app verification captured yet | yes | `AUGMENT_IDE_SCHEME_KIRO` | Keep card visible, show warning, and require copy-link/manual-open until fresh evidence exists. |
| `trae` | `trae` | yes | no | no | Trae.app installed locally, Augment handler compatibility not yet re-verified | yes | `AUGMENT_IDE_SCHEME_TRAE` | Do not auto-launch by default. Use copy-link/manual-open unless operator has explicitly opted in via env override. |
| `windsurf` | `windsurf` | no | no | no | No local app verification captured yet | yes | `AUGMENT_IDE_SCHEME_WINDSURF` | Keep card visible, show warning, and require copy-link/manual-open until fresh evidence exists. |
| `qodo` | `qodo` | no | no | no | No local app verification captured yet | yes | `AUGMENT_IDE_SCHEME_QODO` | Keep card visible, show warning, and require copy-link/manual-open until fresh evidence exists. |
| `codebuddy` | `codebuddy` | no | no | no | No local app verification captured yet | yes | `AUGMENT_IDE_SCHEME_CODEBUDDY` | Keep card visible, show warning, and require copy-link/manual-open until fresh evidence exists. |
| `antigravity` | `antigravity` | no | no | no | No local app verification captured yet | yes | `AUGMENT_IDE_SCHEME_ANTIGRAVITY` | Keep card visible, show warning, and require copy-link/manual-open until fresh evidence exists. |

## Evidence Notes

- `vscode://Augment.vscode-augment/autoAuth` is the existing production path already emitted by the backend.
- `cursor://` was verified locally from an installed Cursor app bundle.
- `trae://` was verified locally from an installed Trae app bundle.
- `cursor` and `trae` still have unverified Augment handler metadata in this repo context, so they remain non-default until explicit operator override.
- `kiro`, `windsurf`, `qodo`, `codebuddy`, and `antigravity` currently use tentative scheme defaults only. They require fresh evidence or an explicit operator override before the backend will treat them as launch-eligible.

## Override Behavior

When an operator sets a non-empty override env var for a target scheme, the backend:

- uses the overridden scheme in the generated deeplink;
- treats that target as launch-eligible for response metadata;
- keeps the current deeplink host and auth path defaults: `Augment.vscode-augment` and `/autoAuth`.

Without an override on an unverified target, the backend still returns a deterministic deeplink and target diagnostics, but marks the target as not verified for auto-launch.

## Operator Checklist

1. Verify the target app is actually installed on the machine before enabling any override.
2. If the scheme is not yet verified in this repo, capture fresh evidence first. Do not enable the override based on guesswork.
3. If you opt into an override for a non-default target, verify both deeplink open and Augment `autoAuth` completion before exposing that target to users as auto-launch-ready.
4. If any target remains warning-only, keep copy-link/manual-open available in the UI and do not treat it as production-ready.
