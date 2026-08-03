from __future__ import annotations

import os
from pathlib import Path
from typing import Callable

from .model_picker import (
    ModelPickerPatchError,
    codex_app_is_running,
    inspect_model_picker_app,
    inspect_plugin_auth_gate_app,
    inspect_plugin_curated_visibility_app,
    inspect_plugin_mention_marketplace_app,
    patch_model_picker_app,
    patch_plugin_auth_gate_app,
    patch_plugin_curated_visibility_app,
    patch_plugin_mention_marketplace_app,
    restore_latest_model_picker_backup,
    restore_latest_plugin_auth_gate_backup,
    restore_latest_plugin_curated_visibility_backup,
    restore_latest_plugin_mention_marketplace_backup,
)

ENHANCEMENT_ORDER = ("model-picker", "plugin-auth-gate", "plugin-mention-marketplace", "plugin-curated-visibility")


def inspect_codex_enhancements(app_path: Path) -> dict[str, object]:
    return {
        "status": "ok",
        "app_path": str(app_path),
        "items": {
            "model-picker": _safe(lambda: inspect_model_picker_app(app_path)),
            "plugin-auth-gate": _safe(lambda: inspect_plugin_auth_gate_app(app_path)),
            "plugin-mention-marketplace": _safe(lambda: inspect_plugin_mention_marketplace_app(app_path)),
            "plugin-curated-visibility": _safe(lambda: inspect_plugin_curated_visibility_app(app_path)),
        },
        "running_app_detected": codex_app_is_running(app_path),
    }


def patch_codex_enhancements(app_path: Path, *, item: str = "all") -> dict[str, object]:
    if codex_app_is_running(app_path):
        already_patched = _already_patched_running_app_result(app_path, item)
        if already_patched is not None:
            return already_patched
        return {
            "status": "app_running_blocking_change",
            "app_path": str(app_path),
            "message": "Codex App is running; quit it before patching enhancements.",
            "restart_required": False,
        }
    preflight = _preflight_app_patch(app_path)
    if preflight is not None:
        return preflight
    results = _run_for_items(
        app_path,
        item,
        {
            "model-picker": lambda: patch_model_picker_app(app_path),
            "plugin-auth-gate": lambda: patch_plugin_auth_gate_app(app_path),
            "plugin-mention-marketplace": lambda: patch_plugin_mention_marketplace_app(app_path),
            "plugin-curated-visibility": lambda: patch_plugin_curated_visibility_app(app_path),
        },
    )
    if any(str(result.get("status")) == "app_bundle_not_writable" for result in results.values()):
        status = "app_bundle_not_writable"
    else:
        status = "patched" if all(str(result.get("status")) in {"patched", "already_patched", "integrity_repaired"} for result in results.values()) else "failed"
    return {
        "status": status,
        "app_path": str(app_path),
        "item": item,
        "items": results,
        "restart_required": status == "patched",
    }


def _already_patched_running_app_result(app_path: Path, item: str) -> dict[str, object] | None:
    inspected = inspect_codex_enhancements(app_path)
    items = inspected.get("items")
    if not isinstance(items, dict):
        return None
    selected: dict[str, dict[str, object]] = {}
    for name in _selected_item_names(item):
        raw = items.get(name)
        if not isinstance(raw, dict) or raw.get("status") != "patched":
            return None
        selected[name] = raw
    return {
        "status": "patched",
        "app_path": str(app_path),
        "item": item,
        "items": selected,
        "running_app_detected": True,
        "restart_required": False,
    }


def _selected_item_names(item: str) -> tuple[str, ...]:
    return ENHANCEMENT_ORDER if item == "all" else (item,)


def restore_codex_enhancements(app_path: Path, *, item: str = "all") -> dict[str, object]:
    if codex_app_is_running(app_path):
        return {
            "status": "app_running_blocking_change",
            "app_path": str(app_path),
            "message": "Codex App is running; quit it before restoring enhancements.",
            "restart_required": False,
        }
    writable_error = _preflight_app_writable(app_path)
    if writable_error is not None:
        return writable_error
    results = _run_for_items(
        app_path,
        item,
        {
            "model-picker": lambda: restore_latest_model_picker_backup(app_path),
            "plugin-auth-gate": lambda: restore_latest_plugin_auth_gate_backup(app_path),
            "plugin-mention-marketplace": lambda: restore_latest_plugin_mention_marketplace_backup(app_path),
            "plugin-curated-visibility": lambda: restore_latest_plugin_curated_visibility_backup(app_path),
        },
    )
    if any(str(result.get("status")) == "app_bundle_not_writable" for result in results.values()):
        status = "app_bundle_not_writable"
    else:
        status = "restored" if all(str(result.get("status")) in {"restored", "not_patched"} for result in results.values()) else "failed"
    return {
        "status": status,
        "app_path": str(app_path),
        "item": item,
        "items": results,
        "restart_required": status == "restored",
    }


def _run_for_items(app_path: Path, item: str, operations: dict[str, Callable[[], dict[str, object]]]) -> dict[str, dict[str, object]]:
    results: dict[str, dict[str, object]] = {}
    for name in _selected_item_names(item):
        operation = operations.get(name)
        if operation is None:
            results[name] = {"status": "failed", "message": f"unknown enhancement item: {name}"}
            continue
        results[name] = _safe(operation)
    return results


def _safe(operation: Callable[[], dict[str, object]]) -> dict[str, object]:
    try:
        return operation()
    except ModelPickerPatchError as err:
        return {"status": "failed", "message": str(err)}
    except PermissionError as err:
        return {"status": "app_bundle_not_writable", "message": str(err)}
    except OSError as err:
        if getattr(err, "errno", None) in {13, 30}:
            return {"status": "app_bundle_not_writable", "message": str(err)}
        return {"status": "failed", "message": str(err)}


def _preflight_app_patch(app_path: Path) -> dict[str, object] | None:
    return _preflight_app_writable(app_path)


def _preflight_app_writable(app_path: Path) -> dict[str, object] | None:
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    plist_path = app_path / "Contents" / "Info.plist"
    backup_parent = app_path.parent
    if not app_path.exists() or not asar_path.exists():
        return None
    if (
        not os.access(app_path, os.W_OK)
        or not os.access(asar_path, os.W_OK)
        or (plist_path.exists() and not os.access(plist_path, os.W_OK))
        or not os.access(backup_parent, os.W_OK)
    ):
        return {
            "status": "app_bundle_not_writable",
            "app_path": str(app_path),
            "message": "Codex App bundle is not writable by the current user.",
            "restart_required": False,
        }
    return None
