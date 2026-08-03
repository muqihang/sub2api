# Codex Desktop Model Picker Injection

This document is the implementation note for adding a model picker patch to the
`zhumeng-agent` injection tool.

Status: updated after live validation on macOS. The patch is known to work only
when both Electron asar integrity layers are updated correctly.

## Problem

Codex Gateway can expose GPT and non-OpenAI Responses models through
`model_catalog_json` and Codex app-server `model/list`. The app-server response
can correctly include `deepseek-v4-pro` and `deepseek-v4-flash` with
`displayName` and `hidden=false`, but Codex Desktop webview may still show the
current model as `Custom` and omit DeepSeek from the picker.

The webview applies an OpenAI availability allowlist in the model query layer.
When that allowlist path is active, local catalog models are filtered even when
`hidden=false`.

## Target Behavior

Patch the Desktop webview so the picker keeps every app-server model with
`hidden=false`, while still allowing official hidden models through the
availability allowlist.

Expected behavior:

- `deepseek-v4-pro` displays as `DeepSeek V4 Pro`, not `Custom`.
- The picker lists `deepseek-v4-pro` and `deepseek-v4-flash`.
- Users can switch GPT -> DeepSeek -> GPT from the UI.
- Future provider models such as GLM or Qwen appear automatically when the
  gateway catalog returns them as visible. Do not hard-code DeepSeek IDs.

## Live Validation Result

The first implementation attempt failed because it only rewrote bytes in
`app.asar` and then wrote `ElectronAsarIntegrity` as the full-file SHA256.
Electron does not use the full-file hash here.

Observed failures:

```text
FATAL:electron/shell/common/asar/asar_util.cc:143
Integrity check failed for asar archive
```

and, after fixing only the top-level hash:

```text
FATAL:electron/shell/browser/net/asar/asar_file_validator.cc:129
Failed to validate block while ending ASAR file stream: 0
```

Root cause:

- `Info.plist -> ElectronAsarIntegrity -> Resources/app.asar -> hash` is the
  SHA256 of the asar header JSON bytes, not SHA256 of the whole `app.asar`.
- Each packed file entry in the asar header can also have
  `integrity.hash` and `integrity.blocks`. If bytes inside a packed file are
  changed, that file entry's integrity metadata must be recalculated.

The working patch sequence is:

1. Patch the minified expression in place.
2. Find the asar file entry that covers the patched byte offset.
3. Recalculate that entry's `integrity.hash` and `integrity.blocks` from the
   patched file content.
4. Serialize the asar header back to JSON. The serialized length must remain
   exactly equal to the original header JSON byte length.
5. Write the new header JSON bytes back into `app.asar`.
6. Set `ElectronAsarIntegrity.Resources/app.asar.hash` to SHA256 of the new
   header JSON bytes.
7. Re-sign the app.

## Verified Patch Point

In one tested Codex Desktop build, the target was inside `app.asar`:

```text
webview/assets/model-queries-jfeupLp0.js
```

In another tested build, the same logic was in:

```text
webview/assets/model-queries-DWZYYu1r.js
```

Do not rely on either hashed file name. Always discover the containing file
from the asar header after finding the byte offset.

The minified filter expression is:

```js
if(l?a.has(e.model):!e.hidden){
```

Inferred variable mapping:

- `e`: model returned by app-server.
- `e.model`: model id.
- `e.hidden`: app-server hidden flag.
- `l`: `shouldUseAvailabilityAllowlist`.
- `a`: availability allowlist `Set`.

The desired readable logic is:

```js
if (!model.hidden || (shouldUseAvailabilityAllowlist && allowlist.has(model.model))) {
```

For a safe in-place `app.asar` byte patch, use this equal-length replacement:

```js
if(!e.hidden|l&a.has(e.model)){
```

Both strings are 31 bytes:

```text
old: if(l?a.has(e.model):!e.hidden){
new: if(!e.hidden|l&a.has(e.model)){
```

The bitwise operators intentionally coerce booleans to `0/1`, which is valid in
an `if` condition:

- non-hidden model: visible because `!e.hidden` is `1`.
- hidden model: visible only when `l & a.has(e.model)` is `1`.

## Why In-Place Patch

Do not fully extract and repack `app.asar` unless the implementation preserves
Electron asar `unpacked` metadata. A plain repack can lose the relationship with
`app.asar.unpacked`, causing native modules to fail at runtime and making Codex
Desktop crash on startup.

The default implementation should patch the bytes in place:

1. Read `app.asar` into a `Buffer`.
2. Search for `Buffer.from("if(l?a.has(e.model):!e.hidden){")`.
3. Require exactly one match.
4. Require replacement byte length to equal original byte length.
5. Copy `Buffer.from("if(!e.hidden|l&a.has(e.model)){")` at the matched offset.
6. Update the containing asar file entry's integrity metadata.
7. Update `Info.plist` with the asar header JSON hash.
8. Re-sign the app.
9. Verify the app starts.

Do not use `shasum -a 256 app.asar` as the `ElectronAsarIntegrity` value. That
will make Electron reject the app at startup.

## Asar Integrity Algorithm

The implementation can parse the asar container without an external library.

Header layout used by Electron asar:

```text
uint32 header_size_pickle
uint32 header_size
uint32 header_json_size_with_padding
uint32 header_json_size
bytes  header_json
```

Practical offsets:

- `headerPickleSize = buffer.readUInt32LE(4)`
- `headerJsonSize = buffer.readUInt32LE(12)`
- `headerJsonStart = 16`
- `fileDataStart = 8 + headerPickleSize`

Implementation details:

1. Parse `JSON.parse(buffer.subarray(16, 16 + headerJsonSize).toString())`.
2. Walk `header.files` recursively.
3. For each file entry with `offset` and `size`, compute:

```js
const start = fileDataStart + Number(entry.offset);
const end = start + Number(entry.size);
```

4. The patched byte offset must fall inside exactly one `[start, end)` range.
5. Recalculate that entry:

```js
const content = buffer.subarray(start, end);
const blockSize = entry.integrity.blockSize || 4194304;
entry.integrity.algorithm = "SHA256";
entry.integrity.hash = sha256(content);
entry.integrity.blocks = chunk(content, blockSize).map(sha256);
```

6. Serialize with `JSON.stringify(header)`.
7. Require `Buffer.byteLength(nextHeaderJson) === headerJsonSize`.
8. Copy the new header JSON bytes into the original buffer at offset `16`.
9. Write the buffer back.

Top-level `ElectronAsarIntegrity` value:

```js
const headerHash = sha256(buffer.subarray(16, 16 + headerJsonSize));
```

Set this into:

```text
Info.plist:ElectronAsarIntegrity:Resources/app.asar:hash
```

## Auto-Discovery

Do not rely on the hashed file name. The tool should:

1. Locate `Contents/Resources/app.asar` for the target Codex `.app`.
2. Parse the asar header.
3. Inspect entries under `webview/assets/model-queries-*.js`.
4. Inspect candidates for these markers:

```text
list-models-for-host
includeHidden:!0
if(l?a.has(e.model):!e.hidden){
```

5. Patch only when the old expression has exactly one match in the whole asar.
6. Derive the target file entry from the matched byte offset, not from the file
   name.
7. If no unique match exists, stop and print the candidate files for manual
   review.

## Idempotency

Recognize these states:

- unpatched: old expression count 1, new expression count 0.
- patched: old expression count 0, new expression count 1.
- unknown: any other combination; stop without writing.

Repeated runs must not patch an already patched app.

## Backup and Restore

Before writing, create:

```text
app.asar.before-model-picker-patch-YYYYMMDD-HHMMSS
```

Restore mode should copy a selected backup over `app.asar` and then verify:

- old expression count is 1.
- new expression count is 0.
- `ElectronAsarIntegrity` has been recalculated from the restored header JSON.
- the app has been re-signed.
- `codesign --verify --deep --strict` succeeds.

Do not delete backups as part of restore.

If restore leaves extra files inside `Contents/Resources`, `codesign --strict`
can fail with a sealed-resource error. Store backups outside the app bundle when
possible. If backups must be placed next to `app.asar`, the restore flow must
either re-sign after creating those files or use a dedicated external backup
directory.

## macOS Permissions

macOS may block writes to `.app/Contents/Resources` with:

```text
Operation not permitted
```

This is usually App Management privacy protection, not a Unix mode problem. The
tool should not silently use `sudo`. Recommended behavior:

- tell the user to grant App Management permission to the host app, or
- copy the target Codex app to a user-writable directory and patch that copy.

## Bundle Identity and Helper Apps

Do not rename the bundle identifier unless the tool also updates every Electron
helper app identifier consistently.

A previous patched copy changed the main bundle id to
`com.openai.codex.patched`; direct launch then failed with:

```text
FATAL:electron/shell/app/electron_main_delegate_mac.mm:65
Unable to find helper app
```

Safest default:

- keep `CFBundleIdentifier = com.openai.codex`;
- keep helper bundle identifiers unchanged;
- keep the `.app` suffix;
- patch a copy first, then move/copy it into `/Applications/Codex.app` only
  after verification.

## Signing

After any write to `app.asar` or `Info.plist`, run:

```bash
codesign --force --deep --sign - /path/to/Codex.app
codesign --verify --deep --strict --verbose=1 /path/to/Codex.app
```

Ad-hoc signing was sufficient in the tested local setup. The tool should still
surface signing failures clearly and avoid continuing to launch a failed bundle.

## Auto-Update

Codex Desktop uses Sparkle. A later app update can replace `app.asar` and remove
the model picker patch.

The injection tool should support:

- `doctor`: report whether the current app is patched and whether the asar
  integrity values match.
- `repair`: re-apply the patch after a Codex update.
- launch-time check: if the app is unpatched or integrity is invalid, stop and
  ask the user to run repair instead of launching a broken app.

## Verification

Code-level checks:

```bash
codesign --verify --deep --strict --verbose=1 /path/to/Codex.app
/usr/libexec/PlistBuddy -c 'Print :ElectronAsarIntegrity:Resources/app.asar:hash' /path/to/Codex.app/Contents/Info.plist
```

Byte checks:

- `app.asar` size unchanged.
- old expression count 0 after patch.
- new expression count 1 after patch.
- the containing `model-queries-*.js` file entry has updated
  `integrity.hash` and `integrity.blocks`.
- `ElectronAsarIntegrity` equals SHA256 of the asar header JSON bytes.

UI checks:

- Desktop opens after restart.
- DeepSeek model names appear in the picker.
- The current DeepSeek model no longer renders as `Custom`.

Runtime checks:

- start `/path/to/Codex.app/Contents/MacOS/Codex` once from a terminal and scan
  stderr for `Integrity check failed`, `Failed to validate block`, and
  `Unable to find helper app`;
- verify the renderer process starts with `--app-path=.../app.asar`;
- verify `codex app-server --analytics-default-enabled` starts.

## Minimal Test Fixtures

The `zhumeng-agent` implementation should have unit tests around these cases:

- unpatched fixture: old expression count 1, patch succeeds, metadata updates;
- patched fixture: old expression count 0, new expression count 1, patch is a
  no-op;
- ambiguous fixture: old expression count greater than 1, patch refuses;
- missing integrity fixture: patch refuses instead of writing a partially valid
  asar;
- restore fixture: restored app has old expression count 1 and matching
  `ElectronAsarIntegrity`;
- bundle id fixture: implementation does not rewrite `CFBundleIdentifier`.

## Reference Implementation

The live-tested shell implementation was originally developed in the
`codex-gateway` worktree:

```text
tools/codex_desktop_model_picker_patch.sh
```

The injection tool should port the algorithm, not shell out to this script as a
permanent dependency. The important behavior to port is the combination of:

- equal-length byte replacement;
- containing-entry discovery from byte offset;
- per-file `integrity.hash` and `integrity.blocks` recalculation;
- `ElectronAsarIntegrity` recalculation from header JSON bytes;
- re-sign and launch verification.
