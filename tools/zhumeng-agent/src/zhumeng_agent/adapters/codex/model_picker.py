from __future__ import annotations

import hashlib
import json
import plistlib
import shutil
import struct
import subprocess
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any


OLD_MODEL_PICKER_EXPR = "if(l?a.has(e.model):!e.hidden){"
NEW_MODEL_PICKER_EXPR = "if(!e.hidden|l&a.has(e.model)){"
CURRENT_MODEL_PICKER_EXPR = "if(d?a.has(e.model):!e.hidden){"
CURRENT_PATCHED_MODEL_PICKER_EXPR = "if(!e.hidden|d&a.has(e.model)){"
CODEX_260609_MODEL_PICKER_EXPR = "if(s?t.has(n.model):!n.hidden){"
CODEX_260609_PATCHED_MODEL_PICKER_EXPR = "if(!n.hidden|s&t.has(n.model)){"
READABLE_PATCHED_MODEL_PICKER_EXPR = "if(!e.hidden||l&&a.has(e.model)){"
OLD_PLUGIN_AUTH_GATE_EXPR = "function e(e){return e!==`chatgpt`}"
NEW_PLUGIN_AUTH_GATE_EXPR = "function e(e){return!1&&e!==`xxxx`}"
CODEX_260609_PLUGIN_AUTH_GATE_EXPR = "function pe(e){return e!==`chatgpt`}"
CODEX_260609_PATCHED_PLUGIN_AUTH_GATE_EXPR = "function pe(e){return!1&&e!==`xxxx`}"
CODEX_260609_PLUGIN_CURATED_HIDE_EXPR = "p&&(h=be)"
CODEX_260609_PATCHED_PLUGIN_CURATED_HIDE_EXPR = "0&&(h=be)"
OLD_PLUGIN_MENTION_MARKETPLACE_EXPR = "additionalMarketplaceKinds:[`shared-with-me`]"
NEW_PLUGIN_MENTION_MARKETPLACE_EXPR = "additionalMarketplaceKinds:void 0/*zhumeng*/ "
CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR = "c={additionalMarketplaceKinds:s?[`shared-with-me`]:[]}"
CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_C_EXPR = "c={additionalMarketplaceKinds:void 0/*zhumeng2*/     }"
CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR = "u={additionalMarketplaceKinds:l?[`shared-with-me`]:[]}"
CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_U_EXPR = "u={additionalMarketplaceKinds:void 0/*zhumeng2*/     }"

MODEL_PICKER_REPLACEMENTS = (
    (OLD_MODEL_PICKER_EXPR, NEW_MODEL_PICKER_EXPR),
    (CURRENT_MODEL_PICKER_EXPR, CURRENT_PATCHED_MODEL_PICKER_EXPR),
    (CODEX_260609_MODEL_PICKER_EXPR, CODEX_260609_PATCHED_MODEL_PICKER_EXPR),
)

PLUGIN_AUTH_GATE_REPLACEMENTS = (
    (OLD_PLUGIN_AUTH_GATE_EXPR, NEW_PLUGIN_AUTH_GATE_EXPR),
    (CODEX_260609_PLUGIN_AUTH_GATE_EXPR, CODEX_260609_PATCHED_PLUGIN_AUTH_GATE_EXPR),
)

PLUGIN_CURATED_VISIBILITY_REPLACEMENTS = (
    (CODEX_260609_PLUGIN_CURATED_HIDE_EXPR, CODEX_260609_PATCHED_PLUGIN_CURATED_HIDE_EXPR),
)

PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS = (
    (OLD_PLUGIN_MENTION_MARKETPLACE_EXPR, NEW_PLUGIN_MENTION_MARKETPLACE_EXPR),
    (CURRENT_PLUGIN_MENTION_MARKETPLACE_C_EXPR, CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_C_EXPR),
    (CURRENT_PLUGIN_MENTION_MARKETPLACE_U_EXPR, CURRENT_PATCHED_PLUGIN_MENTION_MARKETPLACE_U_EXPR),
)


class ModelPickerPatchError(RuntimeError):
    pass


@dataclass
class AsarArchive:
    path: Path
    data: bytearray
    header: dict[str, Any]
    header_json_size: int
    header_start: int
    file_data_start: int

    @property
    def header_bytes(self) -> bytes:
        return bytes(self.data[self.header_start : self.header_start + self.header_json_size])


def inspect_model_picker_app(app_path: Path) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    if not asar_path.exists():
        return {"status": "missing_asar", "integrity_ok": False}

    archive = read_asar(asar_path)
    counts = expression_counts(archive.data)
    status = expression_status(counts)
    target_file = None
    integrity_ok = False
    expression_offset = expression_offset_for_status(archive.data, status)
    if expression_offset is not None:
        entry_match = find_entry_covering_offset(archive, expression_offset)
        if entry_match is not None:
            target_file = entry_match["path"]
            integrity_ok = entry_integrity_ok(archive, entry_match)
    if integrity_ok:
        integrity_ok = plist_hash_ok(plist_path, archive.header_bytes)
    return {
        "status": status,
        "integrity_ok": integrity_ok,
        "old_expression_count": counts["old"],
        "new_expression_count": counts["new"],
        "readable_expression_count": counts["readable"],
        "target_file": target_file,
        "candidate_files": candidate_model_query_files(archive),
    }


def inspect_plugin_auth_gate_app(app_path: Path) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    if not asar_path.exists():
        return {"status": "missing_asar", "integrity_ok": False}

    archive = read_asar(asar_path)
    counts = plugin_auth_gate_counts(archive.data)
    status = plugin_auth_gate_status(counts)
    target_file = None
    integrity_ok = False
    expression_offset = plugin_auth_gate_offset_for_status(archive.data, status)
    if expression_offset is not None:
        entry_match = find_entry_covering_offset(archive, expression_offset)
        if entry_match is not None:
            target_file = entry_match["path"]
            integrity_ok = entry_integrity_ok(archive, entry_match)
    if integrity_ok:
        integrity_ok = plist_hash_ok(plist_path, archive.header_bytes)
    return {
        "status": status,
        "integrity_ok": integrity_ok,
        "old_expression_count": counts["old"],
        "new_expression_count": counts["new"],
        "outside_candidate_expression_count": counts["outside"],
        "target_file": target_file,
        "candidate_files": candidate_plugin_auth_gate_files(archive),
    }


def inspect_plugin_mention_marketplace_app(app_path: Path) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    if not asar_path.exists():
        return {"status": "missing_asar", "integrity_ok": False}

    archive = read_asar(asar_path)
    counts = plugin_mention_marketplace_counts(archive.data)
    status = plugin_mention_marketplace_status(counts)
    target_file = None
    target_files: list[str] = []
    integrity_ok = False
    expressions: list[str] = []
    if status == "unpatched":
        expressions = [old for old, _ in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS]
    elif status == "patched":
        expressions = [new for _, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS]
    if expressions:
        entry_matches = find_plugin_mention_marketplace_entries_for_expressions(archive, expressions)
        if entry_matches:
            target_file = entry_matches[0]["path"]
            target_files = [entry_match["path"] for entry_match in entry_matches]
            integrity_ok = all(entry_integrity_ok(archive, entry_match) for entry_match in entry_matches)
    if integrity_ok:
        integrity_ok = plist_hash_ok(plist_path, archive.header_bytes)
    return {
        "status": status,
        "integrity_ok": integrity_ok,
        "old_expression_count": counts["old"],
        "new_expression_count": counts["new"],
        "outside_candidate_expression_count": counts["outside"],
        "target_file": target_file,
        "target_files": target_files,
        "candidate_files": candidate_plugin_mention_marketplace_files(archive),
    }


def inspect_plugin_curated_visibility_app(app_path: Path) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    if not asar_path.exists():
        return {"status": "missing_asar", "integrity_ok": False}

    archive = read_asar(asar_path)
    counts = plugin_curated_visibility_counts(archive.data)
    status = plugin_curated_visibility_status(counts)
    target_file = None
    integrity_ok = False
    expression_offset = plugin_curated_visibility_offset_for_status(archive.data, status)
    if expression_offset is not None:
        entry_match = find_entry_covering_offset(archive, expression_offset)
        if entry_match is not None:
            target_file = entry_match["path"]
            integrity_ok = entry_integrity_ok(archive, entry_match)
    if integrity_ok:
        integrity_ok = plist_hash_ok(plist_path, archive.header_bytes)
    return {
        "status": status,
        "integrity_ok": integrity_ok,
        "old_expression_count": counts["old"],
        "new_expression_count": counts["new"],
        "outside_candidate_expression_count": counts["outside"],
        "target_file": target_file,
        "candidate_files": candidate_plugin_curated_visibility_files(archive),
    }


def patch_model_picker_app(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    archive = read_asar(asar_path)
    counts = expression_counts(archive.data)

    if counts["old"] == 0 and counts["new"] == 1 and counts["readable"] == 0:
        return ensure_existing_patch_integrity(
            app_path,
            asar_path,
            plist_path,
            archive,
            mode="in_place",
            backup_root=backup_root,
            sign=sign,
            verify_signature=verify_signature,
        )
    if counts["old"] == 0 and counts["new"] == 0 and counts["readable"] == 1:
        return ensure_existing_patch_integrity(
            app_path,
            asar_path,
            plist_path,
            archive,
            mode="readable",
            backup_root=backup_root,
            sign=sign,
            verify_signature=verify_signature,
        )
    if counts["old"] != 1 or counts["new"] != 0 or counts["readable"] != 0:
        raise ModelPickerPatchError(
            "unexpected expression counts "
            f"old={counts['old']} new={counts['new']} readable={counts['readable']}"
        )

    old_expr, new_expr = model_picker_replacement_for_data(archive.data)
    old_bytes = old_expr.encode("utf-8")
    new_bytes = new_expr.encode("utf-8")
    if len(old_bytes) != len(new_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(old_bytes)} != {len(new_bytes)}")

    offset = bytes(archive.data).index(old_bytes)
    entry_match = find_entry_covering_offset(archive, offset)
    if entry_match is None:
        raise ModelPickerPatchError(f"no asar file entry covers patched offset {offset}")
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")

    patched = bytearray(archive.data)
    patched[offset : offset + len(old_bytes)] = new_bytes
    patched_archive = AsarArchive(
        path=archive.path,
        data=patched,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(patched_archive, entry_match)
    write_updated_header(patched_archive)

    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root)
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        patched_archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "patched",
        "app_path": str(app_path),
        "target_file": entry_match["path"],
        "backup_path": str(backup_path),
        "patched_offset": offset,
        "restart_required": True,
    }


def patch_plugin_auth_gate_app(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    archive = read_asar(asar_path)
    counts = plugin_auth_gate_counts(archive.data)

    if counts["old"] == 0 and counts["new"] == 1 and counts["outside"] == 0:
        offset = plugin_auth_gate_offset_for_status(archive.data, "patched")
        if offset is None:
            raise ModelPickerPatchError("cannot locate existing plugin auth gate patch")
        return ensure_existing_byte_patch_integrity(
            app_path,
            asar_path,
            plist_path,
            archive,
            status="patched",
            offset=offset,
            backup_root=backup_root,
            backup_label="plugin-auth-gate",
            sign=sign,
            verify_signature=verify_signature,
        )
    if counts["old"] != 1 or counts["new"] != 0 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "unexpected plugin auth gate expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    old_expr, new_expr = plugin_auth_gate_replacement_for_data(archive.data)
    old_bytes = old_expr.encode("utf-8")
    new_bytes = new_expr.encode("utf-8")
    if len(old_bytes) != len(new_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(old_bytes)} != {len(new_bytes)}")

    entry_match = find_plugin_auth_gate_entry(archive, old_expr)
    offset = entry_match["start"] + bytes(archive.data[entry_match["start"] : entry_match["end"]]).index(old_bytes)

    patched = bytearray(archive.data)
    patched[offset : offset + len(old_bytes)] = new_bytes
    patched_archive = AsarArchive(
        path=archive.path,
        data=patched,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(patched_archive, entry_match)
    write_updated_header(patched_archive)

    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root, label="plugin-auth-gate")
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        patched_archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "patched",
        "app_path": str(app_path),
        "target_file": entry_match["path"],
        "backup_path": str(backup_path),
        "patched_offset": offset,
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }


def patch_plugin_mention_marketplace_app(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    archive = read_asar(asar_path)
    counts = plugin_mention_marketplace_counts(archive.data)

    if counts["old"] == 0 and counts["new"] > 0 and counts["outside"] == 0:
        entry_matches = find_plugin_mention_marketplace_entries_for_expressions(
            archive,
            [new for _, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS],
        )
        if not entry_matches:
            raise ModelPickerPatchError("cannot locate existing plugin mention marketplace patch")
        return ensure_existing_multi_entry_patch_integrity(
            app_path,
            asar_path,
            plist_path,
            archive,
            status="patched",
            entry_matches=entry_matches,
            backup_root=backup_root,
            backup_label="plugin-mention-marketplace",
            sign=sign,
            verify_signature=verify_signature,
        )
    if counts["old"] < 1 or counts["new"] != 0 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "unexpected plugin mention marketplace expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    patched = bytearray(archive.data)
    entry_matches = find_plugin_mention_marketplace_entries_for_expressions(
        archive,
        [old for old, _ in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS],
    )
    offsets = []
    for entry_match in entry_matches:
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        old_expr, new_expr = plugin_mention_marketplace_replacement_for_content(content)
        old_bytes = old_expr.encode("utf-8")
        new_bytes = new_expr.encode("utf-8")
        if len(old_bytes) != len(new_bytes):
            raise ModelPickerPatchError(f"replacement length mismatch {len(old_bytes)} != {len(new_bytes)}")
        offset = entry_match["start"] + content.index(old_bytes)
        offsets.append(offset)
        patched[offset : offset + len(old_bytes)] = new_bytes
    patched_archive = AsarArchive(
        path=archive.path,
        data=patched,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    for entry_match in entry_matches:
        update_entry_integrity(patched_archive, entry_match)
    write_updated_header(patched_archive)

    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root, label="plugin-mention-marketplace")
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        patched_archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "patched",
        "app_path": str(app_path),
        "target_file": entry_matches[0]["path"],
        "target_files": [entry_match["path"] for entry_match in entry_matches],
        "backup_path": str(backup_path),
        "patched_offset": offsets[0],
        "patched_offsets": offsets,
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }


def patch_plugin_curated_visibility_app(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    archive = read_asar(asar_path)
    counts = plugin_curated_visibility_counts(archive.data)

    if counts["old"] == 0 and counts["new"] == 1 and counts["outside"] == 0:
        offset = plugin_curated_visibility_offset_for_status(archive.data, "patched")
        if offset is None:
            raise ModelPickerPatchError("cannot locate existing plugin curated visibility patch")
        return ensure_existing_byte_patch_integrity(
            app_path,
            asar_path,
            plist_path,
            archive,
            status="patched",
            offset=offset,
            backup_root=backup_root,
            backup_label="plugin-curated-visibility",
            sign=sign,
            verify_signature=verify_signature,
        )
    if counts["old"] != 1 or counts["new"] != 0 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "unexpected plugin curated visibility expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    old_expr, new_expr = plugin_curated_visibility_replacement_for_data(archive.data)
    old_bytes = old_expr.encode("utf-8")
    new_bytes = new_expr.encode("utf-8")
    if len(old_bytes) != len(new_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(old_bytes)} != {len(new_bytes)}")

    entry_match = find_plugin_curated_visibility_entry(archive, old_expr)
    content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
    offset = entry_match["start"] + content.index(old_bytes)
    patched = bytearray(archive.data)
    patched[offset : offset + len(old_bytes)] = new_bytes
    patched_archive = AsarArchive(
        path=archive.path,
        data=patched,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(patched_archive, entry_match)
    write_updated_header(patched_archive)

    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root, label="plugin-curated-visibility")
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        patched_archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "patched",
        "app_path": str(app_path),
        "target_file": entry_match["path"],
        "backup_path": str(backup_path),
        "patched_offset": offset,
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }


def restore_latest_model_picker_backup(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    backup = latest_backup(app_path=app_path, backup_root=backup_root)
    if backup is None:
        raise ModelPickerPatchError("no model picker backup found")

    archive = read_asar(asar_path)
    counts = expression_counts(archive.data)
    if counts["old"] != 0 or counts["new"] != 1 or counts["readable"] != 0:
        raise ModelPickerPatchError(
            "current app has unexpected model picker expression counts "
            f"old={counts['old']} new={counts['new']} readable={counts['readable']}"
        )

    new_bytes = NEW_MODEL_PICKER_EXPR.encode("utf-8")
    old_bytes = OLD_MODEL_PICKER_EXPR.encode("utf-8")
    if len(new_bytes) != len(old_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(new_bytes)} != {len(old_bytes)}")
    offset = bytes(archive.data).index(new_bytes)
    entry_match = find_entry_covering_offset(archive, offset)
    if entry_match is None:
        raise ModelPickerPatchError(f"no asar file entry covers restored offset {offset}")
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")

    restored = bytearray(archive.data)
    restored[offset : offset + len(new_bytes)] = old_bytes
    restored_archive = AsarArchive(
        path=archive.path,
        data=restored,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(restored_archive, entry_match)
    write_updated_header(restored_archive)

    restore_backup_path = create_backup(
        asar_path,
        app_path=app_path,
        backup_root=backup_root,
        label="model-picker-restore",
    )
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        restored_archive,
        restore_backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "restored",
        "app_path": str(app_path),
        "backup_path": str(backup),
        "restore_backup_path": str(restore_backup_path),
        "restored_offset": offset,
    }


def restore_latest_plugin_auth_gate_backup(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    backup = latest_backup(app_path=app_path, backup_root=backup_root, label="plugin-auth-gate")
    if backup is None:
        raise ModelPickerPatchError("no plugin auth gate backup found")

    archive = read_asar(asar_path)
    counts = plugin_auth_gate_counts(archive.data)
    if counts["old"] != 0 or counts["new"] != 1 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "current app has unexpected plugin auth gate expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    new_bytes = NEW_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")
    old_bytes = OLD_PLUGIN_AUTH_GATE_EXPR.encode("utf-8")
    if len(new_bytes) != len(old_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(new_bytes)} != {len(old_bytes)}")
    entry_match = find_plugin_auth_gate_entry(archive, NEW_PLUGIN_AUTH_GATE_EXPR)
    offset = entry_match["start"] + bytes(archive.data[entry_match["start"] : entry_match["end"]]).index(new_bytes)
    restored = bytearray(archive.data)
    restored[offset : offset + len(new_bytes)] = old_bytes
    restored_archive = AsarArchive(
        path=archive.path,
        data=restored,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(restored_archive, entry_match)
    write_updated_header(restored_archive)

    restore_backup_path = create_backup(
        asar_path,
        app_path=app_path,
        backup_root=backup_root,
        label="plugin-auth-gate-restore",
    )
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        restored_archive,
        restore_backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "restored",
        "app_path": str(app_path),
        "backup_path": str(backup),
        "restore_backup_path": str(restore_backup_path),
        "restored_offset": offset,
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }


def restore_latest_plugin_curated_visibility_backup(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    require_plist_asar_integrity(plist_path)
    backup = latest_backup(app_path=app_path, backup_root=backup_root, label="plugin-curated-visibility")
    if backup is None:
        raise ModelPickerPatchError("no plugin curated visibility backup found")

    archive = read_asar(asar_path)
    counts = plugin_curated_visibility_counts(archive.data)
    if counts["old"] == 1 and counts["new"] == 0 and counts["outside"] == 0:
        return {"status": "not_patched", "app_path": str(app_path), "backup_path": str(backup), "restart_required": False}
    if counts["old"] != 0 or counts["new"] != 1 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "unexpected plugin curated visibility expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    old_expr, new_expr = plugin_curated_visibility_restore_replacement_for_data(archive.data)
    old_bytes = old_expr.encode("utf-8")
    new_bytes = new_expr.encode("utf-8")
    if len(new_bytes) != len(old_bytes):
        raise ModelPickerPatchError(f"replacement length mismatch {len(new_bytes)} != {len(old_bytes)}")
    entry_match = find_plugin_curated_visibility_entry(archive, new_expr)
    content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
    offset = entry_match["start"] + content.index(new_bytes)
    restored = bytearray(archive.data)
    restored[offset : offset + len(new_bytes)] = old_bytes
    restored_archive = AsarArchive(
        path=archive.path,
        data=restored,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    update_entry_integrity(restored_archive, entry_match)
    write_updated_header(restored_archive)

    restore_backup_path = create_backup(
        asar_path,
        app_path=app_path,
        backup_root=backup_root,
        label="plugin-curated-visibility-restore",
    )
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        restored_archive,
        restore_backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "restored",
        "app_path": str(app_path),
        "backup_path": str(backup),
        "restore_backup_path": str(restore_backup_path),
        "restored_offset": offset,
        "target_file": entry_match["path"],
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }



def restore_latest_plugin_mention_marketplace_backup(
    app_path: Path,
    *,
    backup_root: Path | None = None,
    sign: bool = True,
    verify_signature: bool = True,
) -> dict[str, object]:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    backup = latest_backup(app_path=app_path, backup_root=backup_root, label="plugin-mention-marketplace")
    if codex_app_is_running(app_path):
        raise ModelPickerPatchError("app_running_blocking_change: Codex App is running")
    if backup is None:
        raise ModelPickerPatchError("no plugin mention marketplace backup found")
    require_plist_asar_integrity(plist_path)
    archive = read_asar(asar_path)
    counts = plugin_mention_marketplace_counts(archive.data)
    if counts["new"] == 0 and counts["old"] > 0:
        return {"status": "not_patched", "app_path": str(app_path), "backup_path": str(backup), "restart_required": False}
    if counts["new"] < 1 or counts["old"] != 0 or counts["outside"] != 0:
        raise ModelPickerPatchError(
            "unexpected plugin mention marketplace expression counts "
            f"old={counts['old']} new={counts['new']} outside={counts['outside']}"
        )

    patched_exprs = [new for _, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS]
    entry_matches = find_plugin_mention_marketplace_entries_for_expressions(archive, patched_exprs)
    if not entry_matches:
        raise ModelPickerPatchError("cannot locate plugin mention marketplace patch")

    restored = bytearray(archive.data)
    restored_offsets: list[int] = []
    for entry_match in entry_matches:
        start = archive.file_data_start + int(entry_match["entry"]["offset"])
        size = int(entry_match["entry"]["size"])
        content = bytes(restored[start : start + size])
        old_expr, new_expr = plugin_mention_marketplace_restore_replacement_for_content(content)
        old_bytes = old_expr.encode("utf-8")
        new_bytes = new_expr.encode("utf-8")
        if len(old_bytes) != len(new_bytes):
            raise ModelPickerPatchError(f"restore length mismatch {len(old_bytes)} != {len(new_bytes)}")
        offset = start + content.index(new_bytes)
        restored[offset : offset + len(new_bytes)] = old_bytes
        restored_offsets.append(offset)

    restored_archive = AsarArchive(
        path=archive.path,
        data=restored,
        header=archive.header,
        header_json_size=archive.header_json_size,
        header_start=archive.header_start,
        file_data_start=archive.file_data_start,
    )
    for entry_match in entry_matches:
        update_entry_integrity(restored_archive, entry_match)
    write_updated_header(restored_archive)

    restore_backup_path = create_backup(
        asar_path,
        app_path=app_path,
        backup_root=backup_root,
        label="plugin-mention-marketplace-restore",
    )
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        restored_archive,
        restore_backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "restored",
        "backup_path": str(backup),
        "restore_backup_path": str(restore_backup_path),
        "restored_offset": restored_offsets[0],
        "restored_offsets": restored_offsets,
        "target_file": entry_matches[0]["path"],
        "target_files": [entry_match["path"] for entry_match in entry_matches],
        "running_app_detected": codex_app_is_running(app_path),
        "app_path": str(app_path),
        "restart_required": True,
    }

def ensure_existing_patch_integrity(
    app_path: Path,
    asar_path: Path,
    plist_path: Path,
    archive: AsarArchive,
    *,
    mode: str,
    backup_root: Path | None,
    sign: bool,
    verify_signature: bool,
) -> dict[str, object]:
    status = "patched" if mode == "in_place" else "patched_readable"
    offset = expression_offset_for_status(archive.data, status)
    if offset is None:
        raise ModelPickerPatchError(f"cannot locate existing {mode} model picker patch")
    entry_match = find_entry_covering_offset(archive, offset)
    if entry_match is None:
        raise ModelPickerPatchError(f"no asar file entry covers patched offset {offset}")
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
    if entry_integrity_ok(archive, entry_match) and plist_hash_ok(plist_path, archive.header_bytes):
        return {
            "status": "already_patched",
            "mode": mode,
            "app_path": str(app_path),
            "target_file": entry_match["path"],
        }

    require_plist_asar_integrity(plist_path)
    update_entry_integrity(archive, entry_match)
    write_updated_header(archive)
    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root)
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "integrity_repaired",
        "mode": mode,
        "app_path": str(app_path),
        "target_file": entry_match["path"],
        "backup_path": str(backup_path),
    }


def ensure_existing_byte_patch_integrity(
    app_path: Path,
    asar_path: Path,
    plist_path: Path,
    archive: AsarArchive,
    *,
    status: str,
    offset: int,
    backup_root: Path | None,
    backup_label: str,
    sign: bool,
    verify_signature: bool,
) -> dict[str, object]:
    entry_match = find_entry_covering_offset(archive, offset)
    if entry_match is None:
        raise ModelPickerPatchError(f"no asar file entry covers patched offset {offset}")
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
    if entry_integrity_ok(archive, entry_match) and plist_hash_ok(plist_path, archive.header_bytes):
        result = {
            "status": "already_patched",
            "app_path": str(app_path),
            "target_file": entry_match["path"],
        }
        if backup_label in {"plugin-auth-gate", "plugin-mention-marketplace", "plugin-curated-visibility"}:
            result["restart_required"] = True
            result["running_app_detected"] = codex_app_is_running(app_path)
        return result

    require_plist_asar_integrity(plist_path)
    update_entry_integrity(archive, entry_match)
    write_updated_header(archive)
    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root, label=backup_label)
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    result = {
        "status": "integrity_repaired",
        "mode": status,
        "app_path": str(app_path),
        "target_file": entry_match["path"],
        "backup_path": str(backup_path),
    }
    if backup_label in {"plugin-auth-gate", "plugin-mention-marketplace", "plugin-curated-visibility"}:
        result["restart_required"] = True
        result["running_app_detected"] = codex_app_is_running(app_path)
    return result


def ensure_existing_multi_entry_patch_integrity(
    app_path: Path,
    asar_path: Path,
    plist_path: Path,
    archive: AsarArchive,
    *,
    status: str,
    entry_matches: list[dict[str, Any]],
    backup_root: Path | None,
    backup_label: str,
    sign: bool,
    verify_signature: bool,
) -> dict[str, object]:
    for entry_match in entry_matches:
        if not isinstance(entry_match["entry"].get("integrity"), dict):
            raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
    if all(entry_integrity_ok(archive, entry_match) for entry_match in entry_matches) and plist_hash_ok(
        plist_path,
        archive.header_bytes,
    ):
        return {
            "status": "already_patched",
            "mode": status,
            "app_path": str(app_path),
            "target_file": entry_matches[0]["path"],
            "target_files": [entry_match["path"] for entry_match in entry_matches],
            "restart_required": True,
            "running_app_detected": codex_app_is_running(app_path),
        }

    require_plist_asar_integrity(plist_path)
    for entry_match in entry_matches:
        update_entry_integrity(archive, entry_match)
    write_updated_header(archive)
    backup_path = create_backup(asar_path, app_path=app_path, backup_root=backup_root, label=backup_label)
    write_archive_with_rollback(
        app_path,
        asar_path,
        plist_path,
        archive,
        backup_path,
        sign=sign,
        verify_signature=verify_signature,
    )
    return {
        "status": "integrity_repaired",
        "mode": status,
        "app_path": str(app_path),
        "target_file": entry_matches[0]["path"],
        "target_files": [entry_match["path"] for entry_match in entry_matches],
        "backup_path": str(backup_path),
        "restart_required": True,
        "running_app_detected": codex_app_is_running(app_path),
    }


def read_asar(asar_path: Path) -> AsarArchive:
    archive = read_asar_from_data(asar_path.read_bytes())
    archive.path = asar_path
    return archive


def read_asar_from_data(data: bytes | bytearray) -> AsarArchive:
    data = bytearray(data)
    if len(data) < 16:
        raise ModelPickerPatchError("asar file is too small")
    header_pickle_size = struct.unpack_from("<I", data, 4)[0]
    header_json_size = struct.unpack_from("<I", data, 12)[0]
    header_start = 16
    header_end = header_start + header_json_size
    if header_end > len(data):
        raise ModelPickerPatchError("asar header exceeds file size")
    try:
        header = json.loads(bytes(data[header_start:header_end]).decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ModelPickerPatchError(f"failed to parse asar header: {exc}") from exc
    return AsarArchive(
        path=Path("<memory>"),
        data=data,
        header=header,
        header_json_size=header_json_size,
        header_start=header_start,
        file_data_start=8 + header_pickle_size,
    )


def expression_counts(data: bytes | bytearray) -> dict[str, int]:
    return {
        "old": sum(bytes(data).count(old.encode("utf-8")) for old, _ in MODEL_PICKER_REPLACEMENTS),
        "new": sum(bytes(data).count(new.encode("utf-8")) for _, new in MODEL_PICKER_REPLACEMENTS),
        "readable": bytes(data).count(READABLE_PATCHED_MODEL_PICKER_EXPR.encode("utf-8")),
    }


def plugin_auth_gate_counts(data: bytes | bytearray) -> dict[str, int]:
    archive = read_asar_from_data(data)
    old_exprs = [old.encode("utf-8") for old, _ in PLUGIN_AUTH_GATE_REPLACEMENTS]
    new_exprs = [new.encode("utf-8") for _, new in PLUGIN_AUTH_GATE_REPLACEMENTS]
    counts = {"old": 0, "new": 0, "outside": 0}
    for entry_match in file_entries(archive):
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        old_count = sum(content.count(expr) for expr in old_exprs)
        new_count = sum(content.count(expr) for expr in new_exprs)
        if is_plugin_auth_gate_candidate(entry_match["path"]):
            counts["old"] += old_count
            counts["new"] += new_count
        else:
            counts["outside"] += old_count + new_count
    return counts

def plugin_auth_gate_status(counts: dict[str, int]) -> str:
    if counts.get("outside", 0) != 0:
        return "unknown"
    if counts["old"] == 1 and counts["new"] == 0:
        return "unpatched"
    if counts["old"] == 0 and counts["new"] == 1:
        return "patched"
    return "unknown"


def plugin_auth_gate_offset_for_status(data: bytes | bytearray, status: str) -> int | None:
    archive = read_asar_from_data(data)
    if status == "unpatched":
        return first_plugin_auth_gate_offset(archive, [old for old, _ in PLUGIN_AUTH_GATE_REPLACEMENTS])
    if status == "patched":
        return first_plugin_auth_gate_offset(archive, [new for _, new in PLUGIN_AUTH_GATE_REPLACEMENTS])
    return None



def plugin_curated_visibility_counts(data: bytes | bytearray) -> dict[str, int]:
    archive = read_asar_from_data(data)
    old_exprs = [old.encode("utf-8") for old, _ in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS]
    new_exprs = [new.encode("utf-8") for _, new in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS]
    counts = {"old": 0, "new": 0, "outside": 0}
    for entry_match in file_entries(archive):
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        old_count = sum(content.count(expr) for expr in old_exprs)
        new_count = sum(content.count(expr) for expr in new_exprs)
        if is_plugin_curated_visibility_candidate(entry_match["path"]):
            counts["old"] += old_count
            counts["new"] += new_count
        else:
            counts["outside"] += old_count + new_count
    return counts


def plugin_curated_visibility_status(counts: dict[str, int]) -> str:
    if counts.get("outside", 0) != 0:
        return "unknown"
    if counts["old"] == 1 and counts["new"] == 0:
        return "unpatched"
    if counts["old"] == 0 and counts["new"] == 1:
        return "patched"
    return "unknown"


def plugin_curated_visibility_offset_for_status(data: bytes | bytearray, status: str) -> int | None:
    archive = read_asar_from_data(data)
    if status == "unpatched":
        return first_plugin_curated_visibility_offset(archive, [old for old, _ in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS])
    if status == "patched":
        return first_plugin_curated_visibility_offset(archive, [new for _, new in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS])
    return None


def plugin_mention_marketplace_counts(data: bytes | bytearray) -> dict[str, int]:
    archive = read_asar_from_data(data)
    old_exprs = [old.encode("utf-8") for old, _ in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS]
    new_exprs = [new.encode("utf-8") for _, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS]
    counts = {"old": 0, "new": 0, "outside": 0}
    for entry_match in file_entries(archive):
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        old_count = sum(content.count(expr) for expr in old_exprs)
        new_count = sum(content.count(expr) for expr in new_exprs)
        if is_plugin_mention_marketplace_candidate(entry_match["path"]):
            counts["old"] += old_count
            counts["new"] += new_count
        else:
            counts["outside"] += old_count + new_count
    return counts


def plugin_mention_marketplace_status(counts: dict[str, int]) -> str:
    if counts.get("outside", 0) != 0:
        return "unknown"
    if counts["old"] >= 1 and counts["new"] == 0:
        return "unpatched"
    if counts["old"] == 0 and counts["new"] >= 1:
        return "patched"
    return "unknown"


def plugin_mention_marketplace_offset_for_status(data: bytes | bytearray, status: str) -> int | None:
    archive = read_asar_from_data(data)
    if status == "unpatched":
        return first_plugin_mention_marketplace_offset(
            archive,
            [old for old, _ in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS],
        )
    if status == "patched":
        return first_plugin_mention_marketplace_offset(
            archive,
            [new for _, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS],
        )
    return None


def expression_status(counts: dict[str, int]) -> str:
    if counts["old"] == 1 and counts["new"] == 0 and counts["readable"] == 0:
        return "unpatched"
    if counts["old"] == 0 and counts["new"] == 1 and counts["readable"] == 0:
        return "patched"
    if counts["old"] == 0 and counts["new"] == 0 and counts["readable"] == 1:
        return "patched_readable"
    return "unknown"


def expression_offset_for_status(data: bytes | bytearray, status: str) -> int | None:
    haystack = bytes(data)
    if status == "unpatched":
        return first_expression_offset(haystack, [old for old, _ in MODEL_PICKER_REPLACEMENTS])
    if status == "patched":
        return first_expression_offset(haystack, [new for _, new in MODEL_PICKER_REPLACEMENTS])
    if status == "patched_readable":
        return haystack.index(READABLE_PATCHED_MODEL_PICKER_EXPR.encode("utf-8"))
    return None


def model_picker_replacement_for_data(data: bytes | bytearray) -> tuple[str, str]:
    haystack = bytes(data)
    matches = [(old, new) for old, new in MODEL_PICKER_REPLACEMENTS if haystack.count(old.encode("utf-8")) == 1]
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one model picker replacement candidate, found {len(matches)}")
    return matches[0]


def plugin_auth_gate_replacement_for_data(data: bytes | bytearray) -> tuple[str, str]:
    haystack = bytes(data)
    matches = [(old, new) for old, new in PLUGIN_AUTH_GATE_REPLACEMENTS if haystack.count(old.encode("utf-8")) == 1]
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin auth gate replacement candidate, found {len(matches)}")
    return matches[0]


def plugin_curated_visibility_replacement_for_data(data: bytes | bytearray) -> tuple[str, str]:
    haystack = bytes(data)
    matches = [(old, new) for old, new in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS if haystack.count(old.encode("utf-8")) == 1]
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin curated visibility replacement candidate, found {len(matches)}")
    return matches[0]


def plugin_curated_visibility_restore_replacement_for_data(data: bytes | bytearray) -> tuple[str, str]:
    haystack = bytes(data)
    matches = [(old, new) for old, new in PLUGIN_CURATED_VISIBILITY_REPLACEMENTS if haystack.count(new.encode("utf-8")) == 1]
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin curated visibility restore candidate, found {len(matches)}")
    return matches[0]


def first_expression_offset(haystack: bytes, expressions: list[str]) -> int | None:
    offsets = [haystack.index(expr.encode("utf-8")) for expr in expressions if expr.encode("utf-8") in haystack]
    if not offsets:
        return None
    return min(offsets)


def find_entry_covering_offset(archive: AsarArchive, offset: int) -> dict[str, Any] | None:
    matches: list[dict[str, Any]] = []

    def walk(node: dict[str, Any], path: list[str]) -> None:
        files = node.get("files")
        if isinstance(files, dict):
            for name, child in files.items():
                if isinstance(child, dict):
                    walk(child, [*path, name])
        if "offset" in node and "size" in node:
            start = archive.file_data_start + int(node["offset"])
            end = start + int(node["size"])
            if start <= offset < end:
                matches.append({"entry": node, "path": "/".join(path), "start": start, "end": end})

    walk(archive.header, [])
    if len(matches) > 1:
        raise ModelPickerPatchError(f"patched offset {offset} matched multiple asar entries")
    return matches[0] if matches else None


def update_entry_integrity(archive: AsarArchive, entry_match: dict[str, Any]) -> None:
    entry = entry_match["entry"]
    integrity = entry["integrity"]
    content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
    block_size = int(integrity.get("blockSize") or 4194304)
    integrity["algorithm"] = "SHA256"
    integrity["hash"] = sha256(content)
    integrity["blockSize"] = block_size
    integrity["blocks"] = [sha256(content[index : index + block_size]) for index in range(0, len(content), block_size)]


def write_updated_header(archive: AsarArchive) -> None:
    next_header = json.dumps(archive.header, separators=(",", ":")).encode("utf-8")
    if len(next_header) != archive.header_json_size:
        raise ModelPickerPatchError(
            f"asar header length changed {len(next_header)} != {archive.header_json_size}"
        )
    archive.data[archive.header_start : archive.header_start + archive.header_json_size] = next_header


def entry_integrity_ok(archive: AsarArchive, entry_match: dict[str, Any]) -> bool:
    entry = entry_match["entry"]
    integrity = entry.get("integrity")
    if not isinstance(integrity, dict):
        return False
    content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
    block_size = int(integrity.get("blockSize") or 4194304)
    blocks = [sha256(content[index : index + block_size]) for index in range(0, len(content), block_size)]
    return (
        integrity.get("algorithm") == "SHA256"
        and integrity.get("hash") == sha256(content)
        and integrity.get("blocks") == blocks
    )


def update_plist_header_hash(plist_path: Path, header_bytes: bytes) -> str:
    info, asar_integrity = require_plist_asar_integrity(plist_path)
    header_hash = sha256(header_bytes)
    asar_integrity["hash"] = header_hash
    with plist_path.open("wb") as handle:
        plistlib.dump(info, handle)
    return header_hash


def require_plist_asar_integrity(plist_path: Path) -> tuple[dict[str, Any], dict[str, Any]]:
    try:
        info = plistlib.loads(plist_path.read_bytes())
    except Exception as exc:
        raise ModelPickerPatchError(f"failed to read Info.plist: {exc}") from exc
    integrity = info.get("ElectronAsarIntegrity")
    if not isinstance(integrity, dict):
        raise ModelPickerPatchError("missing ElectronAsarIntegrity in Info.plist")
    asar_integrity = integrity.get("Resources/app.asar")
    if not isinstance(asar_integrity, dict):
        raise ModelPickerPatchError("missing ElectronAsarIntegrity Resources/app.asar in Info.plist")
    return info, asar_integrity


def plist_hash_ok(plist_path: Path, header_bytes: bytes) -> bool:
    if not plist_path.exists():
        return False
    try:
        info = plistlib.loads(plist_path.read_bytes())
    except Exception:
        return False
    return (
        info.get("ElectronAsarIntegrity", {})
        .get("Resources/app.asar", {})
        .get("hash")
        == sha256(header_bytes)
    )


def candidate_model_query_files(archive: AsarArchive) -> list[str]:
    candidates: list[str] = []

    def walk(node: dict[str, Any], path: list[str]) -> None:
        files = node.get("files")
        if isinstance(files, dict):
            for name, child in files.items():
                if isinstance(child, dict):
                    walk(child, [*path, name])
        if "offset" in node and "size" in node:
            file_path = "/".join(path)
            if is_model_picker_candidate(file_path):
                candidates.append(file_path)

    walk(archive.header, [])
    return candidates


def candidate_plugin_auth_gate_files(archive: AsarArchive) -> list[str]:
    return [entry["path"] for entry in file_entries(archive) if is_plugin_auth_gate_candidate(entry["path"])]


def candidate_plugin_curated_visibility_files(archive: AsarArchive) -> list[str]:
    return [entry["path"] for entry in file_entries(archive) if is_plugin_curated_visibility_candidate(entry["path"])]


def is_model_picker_candidate(file_path: str) -> bool:
    return (
        file_path.startswith("webview/assets/model-queries-")
        or file_path.startswith("webview/assets/models-and-reasoning-efforts-")
    ) and file_path.endswith(".js")


def candidate_plugin_mention_marketplace_files(archive: AsarArchive) -> list[str]:
    return [entry["path"] for entry in file_entries(archive) if is_plugin_mention_marketplace_candidate(entry["path"])]


def is_plugin_auth_gate_candidate(file_path: str) -> bool:
    return (
        file_path.startswith("webview/assets/gradient-")
        or file_path.startswith("webview/assets/plugin-auth-")
        or file_path.startswith("webview/assets/use-plugins-")
    ) and file_path.endswith(".js")


def is_plugin_curated_visibility_candidate(file_path: str) -> bool:
    return file_path.startswith("webview/assets/use-plugins-") and file_path.endswith(".js")


def is_plugin_mention_marketplace_candidate(file_path: str) -> bool:
    return (
        file_path.startswith("webview/assets/app-prefetch-impl-")
        or file_path.startswith("webview/assets/inline-mentions-")
        or file_path.startswith("webview/assets/mention-metadata-syncer-")
        or file_path.startswith("webview/assets/prosemirror-")
        or file_path.startswith("webview/assets/reply-")
    ) and file_path.endswith(".js")


def find_plugin_auth_gate_entry(archive: AsarArchive, expression: str) -> dict[str, Any]:
    expression_bytes = expression.encode("utf-8")
    matches = []
    for entry_match in file_entries(archive):
        if not is_plugin_auth_gate_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        if content.count(expression_bytes) > 0:
            matches.append(entry_match)
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin auth gate candidate match, found {len(matches)}")
    entry_match = matches[0]
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
    return entry_match


def find_plugin_curated_visibility_entry(archive: AsarArchive, expression: str) -> dict[str, Any]:
    expression_bytes = expression.encode("utf-8")
    matches = []
    for entry_match in file_entries(archive):
        if not is_plugin_curated_visibility_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        if content.count(expression_bytes) > 0:
            matches.append(entry_match)
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin curated visibility candidate match, found {len(matches)}")
    entry_match = matches[0]
    if not isinstance(entry_match["entry"].get("integrity"), dict):
        raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
    return entry_match


def find_plugin_mention_marketplace_entry(archive: AsarArchive, expression: str) -> dict[str, Any]:
    matches = find_plugin_mention_marketplace_entries(archive, expression)
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin mention marketplace candidate match, found {len(matches)}")
    return matches[0]


def find_plugin_mention_marketplace_entries(archive: AsarArchive, expression: str) -> list[dict[str, Any]]:
    return find_plugin_mention_marketplace_entries_for_expressions(archive, [expression])


def find_plugin_mention_marketplace_entries_for_expressions(
    archive: AsarArchive,
    expressions: list[str],
) -> list[dict[str, Any]]:
    expression_bytes = [expression.encode("utf-8") for expression in expressions]
    matches = []
    for entry_match in file_entries(archive):
        if not is_plugin_mention_marketplace_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        match_count = sum(content.count(expr) for expr in expression_bytes)
        if match_count > 0:
            if match_count != 1:
                raise ModelPickerPatchError(
                    f"expected one plugin mention marketplace expression in {entry_match['path']}, "
                    f"found {match_count}"
                )
            if not isinstance(entry_match["entry"].get("integrity"), dict):
                raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
            matches.append(entry_match)
    return matches


def plugin_mention_marketplace_replacement_for_content(content: bytes) -> tuple[str, str]:
    matches = [
        (old, new)
        for old, new in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS
        if content.count(old.encode("utf-8")) == 1
    ]
    if len(matches) != 1:
        raise ModelPickerPatchError(f"expected one plugin mention marketplace replacement candidate, found {len(matches)}")
    return matches[0]



def plugin_mention_marketplace_restore_replacement_for_content(content: bytes) -> tuple[str, str]:
    for old_expr, new_expr in PLUGIN_MENTION_MARKETPLACE_REPLACEMENTS:
        if new_expr.encode("utf-8") in content:
            return old_expr, new_expr
    raise ModelPickerPatchError("cannot determine plugin mention marketplace restore replacement")

def first_plugin_mention_marketplace_offset(archive: AsarArchive, expressions: list[str]) -> int | None:
    offsets = []
    expression_bytes = [expression.encode("utf-8") for expression in expressions]
    for entry_match in file_entries(archive):
        if not is_plugin_mention_marketplace_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        for expr in expression_bytes:
            if expr in content:
                offsets.append(entry_match["start"] + content.index(expr))
    if len(offsets) != 1:
        return None
    return offsets[0]


def find_plugin_mention_marketplace_entries_legacy(archive: AsarArchive, expression: str) -> list[dict[str, Any]]:
    expression_bytes = expression.encode("utf-8")
    matches = []
    for entry_match in file_entries(archive):
        if not is_plugin_mention_marketplace_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        if content.count(expression_bytes) > 0:
            if content.count(expression_bytes) != 1:
                raise ModelPickerPatchError(
                    f"expected one plugin mention marketplace expression in {entry_match['path']}, "
                    f"found {content.count(expression_bytes)}"
                )
            if not isinstance(entry_match["entry"].get("integrity"), dict):
                raise ModelPickerPatchError(f"asar file entry has no integrity metadata: {entry_match['path']}")
            matches.append(entry_match)
    return matches


def first_plugin_auth_gate_offset(archive: AsarArchive, expressions: list[str]) -> int | None:
    expression_bytes = [expression.encode("utf-8") for expression in expressions]
    offsets = []
    for entry_match in file_entries(archive):
        if not is_plugin_auth_gate_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        for expr in expression_bytes:
            index = content.find(expr)
            if index >= 0:
                offsets.append(entry_match["start"] + index)
    if len(offsets) != 1:
        return None
    return offsets[0]


def first_plugin_curated_visibility_offset(archive: AsarArchive, expressions: list[str]) -> int | None:
    expression_bytes = [expression.encode("utf-8") for expression in expressions]
    offsets = []
    for entry_match in file_entries(archive):
        if not is_plugin_curated_visibility_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        for expr in expression_bytes:
            index = content.find(expr)
            if index >= 0:
                offsets.append(entry_match["start"] + index)
    if len(offsets) != 1:
        return None
    return offsets[0]


def find_plugin_mention_marketplace_offset(archive: AsarArchive, expression: str) -> int | None:
    expression_bytes = expression.encode("utf-8")
    offsets = []
    for entry_match in file_entries(archive):
        if not is_plugin_mention_marketplace_candidate(entry_match["path"]):
            continue
        content = bytes(archive.data[entry_match["start"] : entry_match["end"]])
        index = content.find(expression_bytes)
        if index >= 0:
            offsets.append(entry_match["start"] + index)
    if len(offsets) != 1:
        return None
    return offsets[0]


def file_entries(archive: AsarArchive) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []

    def walk(node: dict[str, Any], path: list[str]) -> None:
        files = node.get("files")
        if isinstance(files, dict):
            for name, child in files.items():
                if isinstance(child, dict):
                    walk(child, [*path, name])
        if "offset" in node and "size" in node:
            start = archive.file_data_start + int(node["offset"])
            end = start + int(node["size"])
            entries.append({"entry": node, "path": "/".join(path), "start": start, "end": end})

    walk(archive.header, [])
    return entries


def create_backup(asar_path: Path, *, app_path: Path, backup_root: Path | None, label: str = "model-picker") -> Path:
    root = backup_root or app_path.parent / ".zhumeng-agent-backups" / app_path.stem
    root.mkdir(parents=True, exist_ok=True)
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    backup_path = root / f"app.asar.before-{label}-patch-{stamp}"
    shutil.copy2(asar_path, backup_path)
    return backup_path


def latest_backup(*, app_path: Path, backup_root: Path | None, label: str = "model-picker") -> Path | None:
    root = backup_root or app_path.parent / ".zhumeng-agent-backups" / app_path.stem
    backups = sorted(root.glob(f"app.asar.before-{label}-patch-*"), key=lambda path: path.stat().st_mtime, reverse=True)
    return backups[0] if backups else None


def write_archive_with_rollback(
    app_path: Path,
    asar_path: Path,
    plist_path: Path,
    archive: AsarArchive,
    backup_path: Path,
    *,
    sign: bool,
    verify_signature: bool,
) -> None:
    original_plist = plist_path.read_bytes()
    try:
        asar_path.write_bytes(archive.data)
        update_plist_header_hash(plist_path, archive.header_bytes)
        if sign:
            sign_app(app_path)
        if verify_signature:
            verify_app_signature(app_path)
    except Exception as exc:
        try:
            shutil.copy2(backup_path, asar_path)
            plist_path.write_bytes(original_plist)
        except Exception as rollback_exc:
            raise ModelPickerPatchError(
                f"patch failed and rollback failed: {exc}; rollback error: {rollback_exc}"
            ) from exc
        raise ModelPickerPatchError(f"patch failed and was rolled back: {exc}") from exc


def codex_app_is_running(app_path: Path) -> bool:
    executable = app_path / "Contents" / "MacOS" / "Codex"
    if not executable.exists():
        return False
    try:
        result = subprocess.run(
            ["pgrep", "-f", str(executable)],
            check=False,
            capture_output=True,
            text=True,
        )
    except FileNotFoundError:
        return False
    return result.returncode == 0


def sign_app(app_path: Path) -> None:
    run_checked(["codesign", "--force", "--deep", "--sign", "-", str(app_path)], failure_label="codesign failed")


def verify_app_signature(app_path: Path) -> None:
    run_checked(
        ["codesign", "--verify", "--deep", "--strict", "--verbose=1", str(app_path)],
        failure_label="codesign verification failed",
    )


def run_checked(command: list[str], *, failure_label: str) -> None:
    try:
        subprocess.run(command, check=True, capture_output=True, text=True)
    except FileNotFoundError as exc:
        raise ModelPickerPatchError(f"{failure_label}: command not found: {command[0]}") from exc
    except subprocess.CalledProcessError as exc:
        detail = (exc.stderr or exc.stdout or str(exc)).strip()
        raise ModelPickerPatchError(f"{failure_label}: {detail}") from exc


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()
