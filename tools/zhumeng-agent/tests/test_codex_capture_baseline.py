import json
import plistlib
import struct
from pathlib import Path

from zhumeng_agent.adapters.codex.capture_baseline import generate_capture_baseline, scan_desktop_bundle_resources
from zhumeng_agent.adapters.codex.capture_config import CodexDesktopCaptureConfig


class FakeRunner:
    def __init__(self, failures: set[tuple[str, ...]] | None = None):
        self.calls: list[list[str]] = []
        self.failures = failures or set()

    def __call__(self, command: list[str]):
        self.calls.append(command)
        if tuple(command) in self.failures:
            return 2, "", "unsupported"
        if command == ["codex", "--version"]:
            return 0, "codex 1.2.3", ""
        if command == ["codex", "app-server", "--help"]:
            return 0, "Usage: codex app-server", ""
        return 0, '{"schema":"ok"}', ""


def make_app(tmp_path: Path) -> Path:
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    (resources / "app.asar").write_bytes(b"asar-bytes")
    with (app / "Contents" / "Info.plist").open("wb") as handle:
        plistlib.dump({"CFBundleShortVersionString": "9.9.9"}, handle)
    return app


def test_baseline_runs_expected_codex_app_server_commands_and_hashes_paths(tmp_path: Path):
    runner = FakeRunner()
    app = make_app(tmp_path)
    out_dir = tmp_path / "baseline"
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=tmp_path / "key")
    config.correlation_hash_key_file.write_bytes(b"shared")

    metadata = generate_capture_baseline(out_dir, app, config, runner=runner)

    assert ["codex", "app-server", "generate-json-schema"] in runner.calls
    assert ["codex", "app-server", "generate-json-schema", "--experimental"] in runner.calls
    assert ["codex", "app-server", "generate-ts"] in runner.calls
    assert ["codex", "app-server", "generate-ts", "--experimental"] in runner.calls
    assert metadata["codex_cli_version"] == "codex 1.2.3"
    assert metadata["desktop_bundle_version"] == "9.9.9"
    assert metadata["desktop_app_path_hash"].startswith("hmac-sha256:")
    assert metadata["desktop_app_path_kind"] in {"custom", "system_applications", "user_home"}
    assert "Codex.app" not in json.dumps(metadata)
    assert str(tmp_path) not in json.dumps(metadata)
    assert (out_dir / "baseline_metadata.json").exists()


def test_baseline_records_help_diagnostic_when_schema_command_fails(tmp_path: Path):
    failing = ("codex", "app-server", "generate-ts", "--experimental")
    runner = FakeRunner(failures={failing})
    app = make_app(tmp_path)

    metadata = generate_capture_baseline(tmp_path / "baseline", app, CodexDesktopCaptureConfig.defaults(), runner=runner)

    assert ["codex", "app-server", "--help"] in runner.calls
    assert metadata["diagnostics"]
    assert metadata["schema_files"]


def make_simple_asar(resources: dict[str, bytes]) -> bytes:
    offset = 0
    files: dict[str, object] = {}
    data = bytearray()
    for resource_path, content in resources.items():
        parts = resource_path.split("/")
        node = files
        for part in parts[:-1]:
            node = node.setdefault(part, {"files": {}})["files"]
        node[parts[-1]] = {"offset": str(offset), "size": len(content)}
        data.extend(content)
        offset += len(content)
    header_json = json.dumps({"files": files}, separators=(",", ":")).encode("utf-8")
    return struct.pack("<IIII", 4, 8 + len(header_json), len(header_json), len(header_json)) + header_json + data


def test_bundle_scanner_records_desktop_builtin_resources_without_host_paths(tmp_path: Path):
    app = tmp_path / "Codex.app"
    resources = app / "Contents" / "Resources"
    resources.mkdir(parents=True)
    (resources / "app.asar").write_bytes(make_simple_asar({
        "webview/assets/model-queries-test.js": b"model picker",
        "webview/assets/app-server-v2.js": b"app-server",
        "plugins/cache/openai-bundled/browser-use/1/.codex-plugin/plugin.json": b"{}",
    }))
    key = tmp_path / "key"
    key.write_bytes(b"shared")
    config = CodexDesktopCaptureConfig.defaults(correlation_hash_key_file=key)

    records = scan_desktop_bundle_resources(app, config)

    assert {record["resource_path"] for record in records} >= {
        "webview/assets/model-queries-test.js",
        "webview/assets/app-server-v2.js",
        "plugins/cache/openai-bundled/browser-use/1/.codex-plugin/plugin.json",
    }
    dumped = json.dumps(records)
    assert str(tmp_path) not in dumped
    assert "/Applications" not in dumped
    assert all(record["source"] == "desktop_bundled_builtin" for record in records)
    assert all(record["content_policy"] == "raw_allowed" for record in records)
    assert all(record["bundle_path_hash"].startswith("hmac-sha256:") for record in records)
