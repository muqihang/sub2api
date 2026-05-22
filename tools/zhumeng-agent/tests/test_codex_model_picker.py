import hashlib
import json
import plistlib
import struct
import subprocess
from pathlib import Path

import pytest

from zhumeng_agent.adapters.codex.model_picker import (
    CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR,
    CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR,
    NEW_PLUGIN_MENTION_MARKETPLACE_EXPR,
    CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_C_EXPR,
    CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_U_EXPR,
    NEW_MODEL_PICKER_EXPR,
    NEW_PLUGIN_AUTH_GATE_EXPR,
    OLD_PLUGIN_MENTION_MARKETPLACE_EXPR,
    OLD_MODEL_PICKER_EXPR,
    OLD_PLUGIN_AUTH_GATE_EXPR,
    READABLE_PATCHED_MODEL_PICKER_EXPR,
    ModelPickerPatchError,
    inspect_model_picker_app,
    inspect_plugin_mention_marketplace_app,
    inspect_plugin_auth_gate_app,
    patch_model_picker_app,
    patch_plugin_mention_marketplace_app,
    patch_plugin_auth_gate_app,
    restore_latest_model_picker_backup,
    restore_latest_plugin_auth_gate_backup,
)


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def make_asar(content: bytes, *, with_integrity: bool = True, filename: str = "model-queries-test.js") -> bytes:
    return make_asar_files({filename: content}, with_integrity=with_integrity)


def make_asar_files(files: dict[str, bytes], *, with_integrity: bool = True) -> bytes:
    entries: dict[str, dict[str, object]] = {}
    offset = 0
    body_parts = []
    for filename, content in files.items():
        entry: dict[str, object] = {
            "size": len(content),
            "offset": str(offset),
        }
        if with_integrity:
            entry["integrity"] = {
                "algorithm": "SHA256",
                "hash": sha256(content),
                "blockSize": 4194304,
                "blocks": [sha256(content)],
            }
        entries[filename] = entry
        body_parts.append(content)
        offset += len(content)
    header = {
        "files": {
            "webview": {
                "files": {
                    "assets": {
                        "files": entries,
                    },
                },
            },
        },
    }
    header_json = json.dumps(header, separators=(",", ":")).encode("utf-8")
    prefix = struct.pack("<IIII", 4, 8 + len(header_json), len(header_json), len(header_json))
    return prefix + header_json + b"".join(body_parts)


def make_single_entry_asar(content: bytes, *, with_integrity: bool = True, filename: str = "model-queries-test.js") -> bytes:
    entry: dict[str, object] = {
        "size": len(content),
        "offset": "0",
    }
    if with_integrity:
        entry["integrity"] = {
            "algorithm": "SHA256",
            "hash": sha256(content),
            "blockSize": 4194304,
            "blocks": [sha256(content)],
        }
    header = {
        "files": {
            "webview": {
                "files": {
                    "assets": {
                        "files": {
                            filename: entry,
                        },
                    },
                },
            },
        },
    }
    header_json = json.dumps(header, separators=(",", ":")).encode("utf-8")
    prefix = struct.pack("<IIII", 4, 8 + len(header_json), len(header_json), len(header_json))
    return prefix + header_json + content


def read_asar_header(asar_path: Path) -> tuple[dict[str, object], bytes]:
    data = asar_path.read_bytes()
    header_json_size = struct.unpack_from("<I", data, 12)[0]
    header_bytes = data[16 : 16 + header_json_size]
    return json.loads(header_bytes.decode("utf-8")), header_bytes


def write_asar_header(asar_path: Path, header: dict[str, object]) -> None:
    data = bytearray(asar_path.read_bytes())
    header_json_size = struct.unpack_from("<I", data, 12)[0]
    header_bytes = json.dumps(header, separators=(",", ":")).encode("utf-8")
    assert len(header_bytes) == header_json_size
    data[16 : 16 + header_json_size] = header_bytes
    asar_path.write_bytes(data)


def model_entry(header: dict[str, object], filename: str = "model-queries-test.js") -> dict[str, object]:
    files = header["files"]
    assert isinstance(files, dict)
    entry = files["webview"]["files"]["assets"]["files"][filename]
    assert isinstance(entry, dict)
    return entry


def make_codex_app(
    tmp_path: Path,
    content: bytes,
    *,
    with_integrity: bool = True,
    filename: str = "model-queries-test.js",
) -> Path:
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    asar = resources / "app.asar"
    asar.write_bytes(make_asar(content, with_integrity=with_integrity, filename=filename))
    _, header_bytes = read_asar_header(asar)
    with (app / "Contents" / "Info.plist").open("wb") as handle:
        plistlib.dump(
            {
                "CFBundleIdentifier": "com.openai.codex",
                "ElectronAsarIntegrity": {
                    "Resources/app.asar": {
                        "hash": sha256(header_bytes),
                    },
                },
            },
            handle,
        )
    return app


def make_codex_app_files(tmp_path: Path, files: dict[str, bytes], *, with_integrity: bool = True) -> Path:
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    asar = resources / "app.asar"
    asar.write_bytes(make_asar_files(files, with_integrity=with_integrity))
    _, header_bytes = read_asar_header(asar)
    with (app / "Contents" / "Info.plist").open("wb") as handle:
        plistlib.dump(
            {
                "CFBundleIdentifier": "com.openai.codex",
                "ElectronAsarIntegrity": {
                    "Resources/app.asar": {
                        "hash": sha256(header_bytes),
                    },
                },
            },
            handle,
        )
    return app


def test_patch_rewrites_model_picker_and_updates_integrity(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )

    result = patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "patched"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert data.count(OLD_MODEL_PICKER_EXPR.encode("utf-8")) == 0
    assert data.count(NEW_MODEL_PICKER_EXPR.encode("utf-8")) == 1
    assert (app / "Contents" / "Resources" / "app.asar.before-model-picker-patch").exists() is False

    header, header_bytes = read_asar_header(asar)
    entry = model_entry(header)
    integrity = entry["integrity"]
    assert isinstance(integrity, dict)
    content_start = 16 + len(header_bytes)
    content = data[content_start:]
    assert integrity["hash"] == sha256(content)
    assert integrity["blocks"] == [sha256(content)]

    info = plistlib.loads((app / "Contents" / "Info.plist").read_bytes())
    assert info["CFBundleIdentifier"] == "com.openai.codex"
    assert info["ElectronAsarIntegrity"]["Resources/app.asar"]["hash"] == sha256(header_bytes)

    status = inspect_model_picker_app(app)
    assert status["status"] == "patched"
    assert status["integrity_ok"] is True
    assert status["target_file"] == "webview/assets/model-queries-test.js"


def test_patch_rewrites_current_model_picker_expression(tmp_path: Path):
    current_expression = "if(d?a.has(e.model):!e.hidden){"
    expected_expression = "if(!e.hidden|d&a.has(e.model)){"
    app = make_codex_app(
        tmp_path,
        b"prefix " + current_expression.encode("utf-8") + b" suffix",
    )

    result = patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "patched"
    data = (app / "Contents" / "Resources" / "app.asar").read_bytes()
    assert data.count(current_expression.encode("utf-8")) == 0
    assert data.count(expected_expression.encode("utf-8")) == 1


def test_patch_is_idempotent_when_expression_is_already_patched(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + NEW_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )

    result = patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "already_patched"
    assert not (tmp_path / "backups").exists()


def test_patch_treats_readable_repacked_expression_as_already_patched(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + READABLE_PATCHED_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )

    result = patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "already_patched"
    assert result["mode"] == "readable"
    assert not (tmp_path / "backups").exists()


def test_patch_repairs_integrity_when_expression_is_already_patched(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + NEW_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )
    asar = app / "Contents" / "Resources" / "app.asar"
    header, _ = read_asar_header(asar)
    integrity = model_entry(header)["integrity"]
    assert isinstance(integrity, dict)
    integrity["hash"] = "0" * 64
    integrity["blocks"] = ["0" * 64]
    write_asar_header(asar, header)
    info = plistlib.loads((app / "Contents" / "Info.plist").read_bytes())
    info["ElectronAsarIntegrity"]["Resources/app.asar"]["hash"] = "0" * 64
    with (app / "Contents" / "Info.plist").open("wb") as handle:
        plistlib.dump(info, handle)

    result = patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "integrity_repaired"
    assert result["mode"] == "in_place"
    status = inspect_model_picker_app(app)
    assert status["status"] == "patched"
    assert status["integrity_ok"] is True


def test_patch_refuses_ambiguous_expression_without_writing(tmp_path: Path):
    original = b" ".join([
        OLD_MODEL_PICKER_EXPR.encode("utf-8"),
        OLD_MODEL_PICKER_EXPR.encode("utf-8"),
    ])
    app = make_codex_app(tmp_path, original)
    asar = app / "Contents" / "Resources" / "app.asar"
    before = asar.read_bytes()

    with pytest.raises(ModelPickerPatchError, match="unexpected expression counts"):
        patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert asar.read_bytes() == before


def test_patch_refuses_missing_file_integrity_without_writing(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
        with_integrity=False,
    )
    asar = app / "Contents" / "Resources" / "app.asar"
    before = asar.read_bytes()

    with pytest.raises(ModelPickerPatchError, match="has no integrity metadata"):
        patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert asar.read_bytes() == before


def test_patch_refuses_missing_plist_integrity_without_writing(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )
    plist_path = app / "Contents" / "Info.plist"
    with plist_path.open("wb") as handle:
        plistlib.dump({"CFBundleIdentifier": "com.openai.codex"}, handle)
    asar = app / "Contents" / "Resources" / "app.asar"
    before = asar.read_bytes()

    with pytest.raises(ModelPickerPatchError, match="missing ElectronAsarIntegrity"):
        patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert asar.read_bytes() == before


def test_patch_wraps_codesign_failure(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )

    def fail_run(*args, **kwargs):
        raise subprocess.CalledProcessError(1, ["codesign"], output="out", stderr="sign failed")

    monkeypatch.setattr("zhumeng_agent.adapters.codex.model_picker.subprocess.run", fail_run)

    with pytest.raises(ModelPickerPatchError, match="codesign failed"):
        patch_model_picker_app(app, backup_root=tmp_path / "backups", sign=True, verify_signature=False)


def test_restore_latest_backup_reverts_patch_and_recalculates_plist_hash(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
    )
    backup_root = tmp_path / "backups"
    patch_model_picker_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    result = restore_latest_model_picker_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    assert result["status"] == "restored"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert data.count(OLD_MODEL_PICKER_EXPR.encode("utf-8")) == 1
    assert data.count(NEW_MODEL_PICKER_EXPR.encode("utf-8")) == 0


def test_plugin_auth_gate_patch_allows_plugin_page_and_updates_integrity(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
        filename="gradient-test.js",
    )

    result = patch_plugin_auth_gate_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "patched"
    assert result["target_file"] == "webview/assets/gradient-test.js"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert data.count(OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")) == 0
    assert data.count(NEW_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")) == 1

    header, header_bytes = read_asar_header(asar)
    entry = model_entry(header, filename="gradient-test.js")
    integrity = entry["integrity"]
    assert isinstance(integrity, dict)
    content_start = 16 + len(header_bytes)
    content = data[content_start:]
    assert integrity["hash"] == sha256(content)
    assert integrity["blocks"] == [sha256(content)]

    status = inspect_plugin_auth_gate_app(app)
    assert status["status"] == "patched"
    assert status["integrity_ok"] is True
    assert status["target_file"] == "webview/assets/gradient-test.js"


def test_plugin_auth_gate_patch_is_idempotent(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + NEW_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
        filename="gradient-test.js",
    )

    result = patch_plugin_auth_gate_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert result["status"] == "already_patched"
    assert not (tmp_path / "backups").exists()


def test_plugin_auth_gate_restore_reverts_patch(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
        filename="gradient-test.js",
    )
    backup_root = tmp_path / "backups"
    patch_plugin_auth_gate_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    result = restore_latest_plugin_auth_gate_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    assert result["status"] == "restored"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert data.count(OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")) == 1
    assert data.count(NEW_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")) == 0
    _, header_bytes = read_asar_header(asar)
    info = plistlib.loads((app / "Contents" / "Info.plist").read_bytes())
    assert info["ElectronAsarIntegrity"]["Resources/app.asar"]["hash"] == sha256(header_bytes)


def test_plugin_auth_gate_restore_does_not_revert_model_picker_patch(tmp_path: Path):
    app = make_codex_app_files(
        tmp_path,
        {
            "gradient-test.js": b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
            "model-queries-test.js": b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
        },
    )
    backup_root = tmp_path / "backups"
    patch_plugin_auth_gate_app(app, backup_root=backup_root, sign=False, verify_signature=False)
    patch_model_picker_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    restore_latest_plugin_auth_gate_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    plugin_status = inspect_plugin_auth_gate_app(app)
    model_status = inspect_model_picker_app(app)
    assert plugin_status["status"] == "unpatched"
    assert plugin_status["integrity_ok"] is True
    assert model_status["status"] == "patched"
    assert model_status["integrity_ok"] is True


def test_model_picker_restore_does_not_revert_plugin_auth_gate_patch(tmp_path: Path):
    app = make_codex_app_files(
        tmp_path,
        {
            "gradient-test.js": b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
            "model-queries-test.js": b"prefix " + OLD_MODEL_PICKER_EXPR.encode("utf-8") + b" suffix",
        },
    )
    backup_root = tmp_path / "backups"
    patch_model_picker_app(app, backup_root=backup_root, sign=False, verify_signature=False)
    patch_plugin_auth_gate_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    restore_latest_model_picker_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    plugin_status = inspect_plugin_auth_gate_app(app)
    model_status = inspect_model_picker_app(app)
    assert plugin_status["status"] == "patched"
    assert plugin_status["integrity_ok"] is True
    assert model_status["status"] == "unpatched"
    assert model_status["integrity_ok"] is True


def test_plugin_auth_gate_patch_refuses_non_candidate_expression_without_writing(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
        filename="not-gradient-test.js",
    )
    asar = app / "Contents" / "Resources" / "app.asar"
    before = asar.read_bytes()

    with pytest.raises(ModelPickerPatchError, match="outside=1"):
        patch_plugin_auth_gate_app(app, backup_root=tmp_path / "backups", sign=False, verify_signature=False)

    assert asar.read_bytes() == before


def test_plugin_auth_gate_patch_rolls_back_when_codesign_fails(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8") + b" suffix",
        filename="gradient-test.js",
    )
    asar = app / "Contents" / "Resources" / "app.asar"
    plist_path = app / "Contents" / "Info.plist"
    before_asar = asar.read_bytes()
    before_plist = plist_path.read_bytes()

    def fail_run(*args, **kwargs):
        raise subprocess.CalledProcessError(1, ["codesign"], output="out", stderr="sign failed")

    monkeypatch.setattr("zhumeng_agent.adapters.codex.model_picker.subprocess.run", fail_run)

    with pytest.raises(ModelPickerPatchError, match="rolled back"):
        patch_plugin_auth_gate_app(app, backup_root=tmp_path / "backups", sign=True, verify_signature=False)

    assert asar.read_bytes() == before_asar
    assert plist_path.read_bytes() == before_plist


def test_plugin_mention_marketplace_patch_enables_default_marketplaces_and_updates_integrity(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
        filename="prosemirror-test.js",
    )

    result = patch_plugin_mention_marketplace_app(
        app,
        backup_root=tmp_path / "backups",
        sign=False,
        verify_signature=False,
    )

    assert result["status"] == "patched"
    assert result["target_file"] == "webview/assets/prosemirror-test.js"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert data.count(OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8")) == 0
    assert data.count(NEW_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8")) == 1

    header, header_bytes = read_asar_header(asar)
    entry = model_entry(header, filename="prosemirror-test.js")
    integrity = entry["integrity"]
    assert isinstance(integrity, dict)
    content_start = 16 + len(header_bytes)
    content = data[content_start:]
    assert integrity["hash"] == sha256(content)
    assert integrity["blocks"] == [sha256(content)]

    status = inspect_plugin_mention_marketplace_app(app)
    assert status["status"] == "patched"
    assert status["integrity_ok"] is True
    assert status["target_file"] == "webview/assets/prosemirror-test.js"


def test_plugin_mention_marketplace_patch_is_idempotent(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + NEW_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
        filename="prosemirror-test.js",
    )

    result = patch_plugin_mention_marketplace_app(
        app,
        backup_root=tmp_path / "backups",
        sign=False,
        verify_signature=False,
    )

    assert result["status"] == "already_patched"
    assert not (tmp_path / "backups").exists()


def test_plugin_mention_marketplace_patch_updates_reply_and_at_menu_chunks(tmp_path: Path):
    app = make_codex_app_files(
        tmp_path,
        {
            "prosemirror-test.js": b"prefix " + OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
            "reply-test.js": b"prefix " + OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
        },
    )

    result = patch_plugin_mention_marketplace_app(
        app,
        backup_root=tmp_path / "backups",
        sign=False,
        verify_signature=False,
    )

    assert result["status"] == "patched"
    assert result["target_files"] == [
        "webview/assets/prosemirror-test.js",
        "webview/assets/reply-test.js",
    ]
    status = inspect_plugin_mention_marketplace_app(app)
    assert status["status"] == "patched"
    assert status["old_expression_count"] == 0
    assert status["new_expression_count"] == 2
    assert status["integrity_ok"] is True


def test_plugin_mention_marketplace_patch_handles_current_flag_gated_chunks(tmp_path: Path):
    app = make_codex_app_files(
        tmp_path,
        {
            "prosemirror-test.js": b"prefix " + CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") + b" suffix",
            "reply-test.js": b"prefix " + CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") + b" suffix",
        },
    )

    result = patch_plugin_mention_marketplace_app(
        app,
        backup_root=tmp_path / "backups",
        sign=False,
        verify_signature=False,
    )

    assert result["status"] == "patched"
    asar = app / "Contents" / "Resources" / "app.asar"
    data = asar.read_bytes()
    assert CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") not in data
    assert CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") not in data
    assert CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") in data
    assert CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") in data

    status = inspect_plugin_mention_marketplace_app(app)
    assert status["status"] == "patched"
    assert status["old_expression_count"] == 0
    assert status["new_expression_count"] == 2
    assert status["integrity_ok"] is True


def test_plugin_mention_marketplace_patch_refuses_non_candidate_expression_without_writing(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
        filename="composer-test.js",
    )
    asar = app / "Contents" / "Resources" / "app.asar"
    before = asar.read_bytes()

    with pytest.raises(ModelPickerPatchError, match="outside=1"):
        patch_plugin_mention_marketplace_app(
            app,
            backup_root=tmp_path / "backups",
            sign=False,
            verify_signature=False,
        )

    assert asar.read_bytes() == before


def test_plugin_mention_marketplace_restore_reverts_patch(tmp_path: Path):
    app = make_codex_app(
        tmp_path,
        b"prefix " + OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8") + b" suffix",
        filename="prosemirror-test.js",
    )
    backup_root = tmp_path / "backups"
    patch_plugin_mention_marketplace_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    from zhumeng_agent.adapters.codex.model_picker import restore_latest_plugin_mention_marketplace_backup

    result = restore_latest_plugin_mention_marketplace_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    assert result["status"] == "restored"
    data = (app / "Contents" / "Resources" / "app.asar").read_bytes()
    assert data.count(OLD_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8")) == 1
    assert data.count(NEW_PLUGIN_MENTION_MARKPLACE_EXPR.encode("utf-8") if False else NEW_PLUGIN_MENTION_MARKETPLACE_EXPR.encode("utf-8")) == 0


def test_plugin_mention_marketplace_restore_handles_current_flag_gated_chunks(tmp_path: Path):
    app = make_codex_app_files(
        tmp_path,
        {
            "prosemirror-test.js": b"prefix " + CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") + b" suffix",
            "reply-test.js": b"prefix " + CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") + b" suffix",
        },
    )
    backup_root = tmp_path / "backups"
    patch_plugin_mention_marketplace_app(app, backup_root=backup_root, sign=False, verify_signature=False)

    from zhumeng_agent.adapters.codex.model_picker import restore_latest_plugin_mention_marketplace_backup

    result = restore_latest_plugin_mention_marketplace_backup(app, backup_root=backup_root, sign=False, verify_signature=False)

    assert result["status"] == "restored"
    data = (app / "Contents" / "Resources" / "app.asar").read_bytes()
    assert CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") in data
    assert CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") in data
    assert CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_C_EXPR.encode("utf-8") not in data
    assert CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_U_EXPR.encode("utf-8") not in data


def test_codex_enhancement_aggregate_refuses_running_app_without_patching(tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    from zhumeng_agent.adapters.codex import enhancements

    app = tmp_path / "Codex.app"
    (app / "Contents" / "Resources").mkdir(parents=True)
    (app / "Contents" / "Resources" / "app.asar").write_bytes(b"dummy")
    monkeypatch.setattr(enhancements, "codex_app_is_running", lambda app_path: True)
    monkeypatch.setattr(enhancements, "patch_model_picker_app", lambda app_path: (_ for _ in ()).throw(AssertionError("must not patch")))

    result = enhancements.patch_codex_enhancements(app, item="all")

    assert result["status"] == "app_running_blocking_change"
    assert result["restart_required"] is False
