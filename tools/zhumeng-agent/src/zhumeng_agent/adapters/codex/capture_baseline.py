from __future__ import annotations

import json
import plistlib
import subprocess
import struct
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable

from .capture_config import CodexDesktopCaptureConfig, CorrelationHasher, file_hash, path_identity

Runner = Callable[[list[str]], tuple[int, str, str]]


BASELINE_COMMANDS = [
    ["codex", "app-server", "generate-json-schema"],
    ["codex", "app-server", "generate-json-schema", "--experimental"],
    ["codex", "app-server", "generate-ts"],
    ["codex", "app-server", "generate-ts", "--experimental"],
]


def default_runner(command: list[str]) -> tuple[int, str, str]:
    completed = subprocess.run(command, capture_output=True, text=True, check=False)
    return completed.returncode, completed.stdout, completed.stderr


def generate_capture_baseline(
    out_dir: Path,
    codex_app_path: Path,
    config: CodexDesktopCaptureConfig,
    *,
    runner: Runner = default_runner,
) -> dict[str, object]:
    config.validate()
    out_dir.mkdir(parents=True, exist_ok=True)
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    schema_files: list[dict[str, object]] = []
    diagnostics: list[dict[str, object]] = []

    code, version_out, _ = runner(["codex", "--version"])
    codex_cli_version = version_out.strip() if code == 0 else "unknown"
    for command in BASELINE_COMMANDS:
        returncode, stdout, stderr = runner(command)
        name = "_".join(command[2:]).replace("--", "")
        output_path = out_dir / f"{name}.txt"
        output_path.write_text(stdout, encoding="utf-8")
        schema_files.append({
            "command": " ".join(command),
            "resource_path": output_path.name,
            "hash": hasher.hash_identifier(stdout),
            "returncode": returncode,
        })
        if returncode != 0:
            help_code, help_out, help_err = runner(["codex", "app-server", "--help"])
            diagnostics.append({
                "command": " ".join(command),
                "returncode": returncode,
                "stderr_hash": hasher.hash_identifier(stderr),
                "help_returncode": help_code,
                "help_hash": hasher.hash_identifier(help_out + help_err),
            })

    metadata = {
        "codex_cli_version": codex_cli_version,
        **path_identity(codex_app_path, hasher),
        "desktop_bundle_version": desktop_bundle_version(codex_app_path),
        "app_asar_hash": file_hash(codex_app_path / "Contents" / "Resources" / "app.asar", hasher),
        "schema_files": schema_files,
        "diagnostics": diagnostics,
        "generated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    (out_dir / "baseline_metadata.json").write_text(json.dumps(metadata, indent=2, sort_keys=True), encoding="utf-8")
    return metadata


def desktop_bundle_version(app_path: Path) -> str | None:
    plist_path = app_path / "Contents" / "Info.plist"
    if not plist_path.exists():
        return None
    try:
        info = plistlib.loads(plist_path.read_bytes())
    except Exception:
        return None
    value = info.get("CFBundleShortVersionString") or info.get("CFBundleVersion")
    return str(value) if value is not None else None


def scan_desktop_bundle_resources(app_path: Path, config: CodexDesktopCaptureConfig) -> list[dict[str, object]]:
    hasher = CorrelationHasher.from_key_file(config.correlation_hash_key_file)
    asar_path = app_path / "Contents" / "Resources" / "app.asar"
    if not asar_path.exists():
        return []
    bundle_hash = file_hash(asar_path, hasher)
    identity = path_identity(app_path, hasher)
    records: list[dict[str, object]] = []
    for resource_path, content in read_asar_resources(asar_path).items():
        if is_capture_relevant_resource(resource_path):
            records.append({
                "schema_version": 1,
                "source": "desktop_bundled_builtin",
                "bundle_path_hash": identity["desktop_app_path_hash"],
                "app_path_kind": identity["desktop_app_path_kind"],
                "resource_path": resource_path,
                "bundle_hash": bundle_hash,
                "resource_hash": hasher.hash_bytes(content),
                "content_policy": "raw_allowed",
            })
    return records


def read_asar_resources(asar_path: Path) -> dict[str, bytes]:
    data = asar_path.read_bytes()
    if len(data) < 16:
        return {}
    try:
        header_pickle_size = struct.unpack_from("<I", data, 4)[0]
        header_json_size = struct.unpack_from("<I", data, 12)[0]
        header = json.loads(data[16 : 16 + header_json_size].decode("utf-8"))
    except Exception:
        return {}
    file_data_start = 8 + header_pickle_size
    resources: dict[str, bytes] = {}

    def walk(node: dict[str, object], path: list[str]) -> None:
        files = node.get("files")
        if isinstance(files, dict):
            for name, child in files.items():
                if isinstance(child, dict):
                    walk(child, [*path, str(name)])
        if "offset" in node and "size" in node:
            start = file_data_start + int(node["offset"])
            end = start + int(node["size"])
            resources["/".join(path)] = data[start:end]

    if isinstance(header, dict):
        walk(header, [])
    return resources


def is_capture_relevant_resource(resource_path: str) -> bool:
    lower = resource_path.lower()
    return (
        lower.startswith("webview/assets/")
        or "app-server" in lower
        or "model-queries" in lower
        or lower.endswith("/.codex-plugin/plugin.json")
        or lower.endswith("app.json")
        or "computer" in lower
        or "browser" in lower
        or "chrome" in lower
    )
