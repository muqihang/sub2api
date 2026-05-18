from pathlib import Path

from zhumeng_agent.adapters.codex.plugins import (
    STATUS_AVAILABLE,
    STATUS_CONFIGURED,
    STATUS_MISSING_BUNDLE,
    STATUS_UNSUPPORTED_OS,
    STATUS_USER_ACTION_REQUIRED,
    inspect_codex_plugins,
)
from zhumeng_agent.doctor import chrome_integration_paths


def write_plugin_manifest(root: Path, plugin_name: str, version: str) -> None:
    manifest = root / "plugins" / "cache" / "openai-bundled" / plugin_name / version / ".codex-plugin" / "plugin.json"
    manifest.parent.mkdir(parents=True, exist_ok=True)
    manifest.write_text('{"name":"%s","version":"%s"}' % (plugin_name, version), encoding="utf-8")


def test_detects_bundled_plugin_manifests_by_version_directory(tmp_path: Path):
    write_plugin_manifest(tmp_path, "browser-use", "0.1.0-alpha2")
    report = inspect_codex_plugins(tmp_path)
    assert report["browser-use"]["status"] == STATUS_AVAILABLE


def test_computer_use_reports_configured_when_helper_exists(tmp_path: Path):
    write_plugin_manifest(tmp_path, "computer-use", "1.0.780")
    helper = tmp_path / "computer-use" / "Codex Computer Use.app"
    helper.mkdir(parents=True, exist_ok=True)
    report = inspect_codex_plugins(tmp_path)
    assert report["computer-use"]["status"] == STATUS_CONFIGURED


def test_computer_use_reports_unsupported_os_when_mcp_client_requires_newer_macos(tmp_path: Path):
    write_plugin_manifest(tmp_path, "computer-use", "1.0.791")
    client = (
        tmp_path
        / "computer-use"
        / "Codex Computer Use.app"
        / "Contents"
        / "SharedSupport"
        / "SkyComputerUseClient.app"
        / "Contents"
        / "MacOS"
        / "SkyComputerUseClient"
    )
    client.parent.mkdir(parents=True, exist_ok=True)
    client.write_text("#!/bin/sh\n", encoding="utf-8")

    def fake_runner(*args, **kwargs):
        class Result:
            returncode = 0
            stdout = """
Load command 11
      cmd LC_BUILD_VERSION
  cmdsize 32
 platform 1
    minos 15.0
      sdk 26.4
"""
            stderr = ""

        return Result()

    report = inspect_codex_plugins(tmp_path, macos_version="14.5", runner=fake_runner)

    assert report["computer-use"]["status"] == STATUS_UNSUPPORTED_OS
    assert report["computer-use"]["current_macos"] == "14.5"
    assert report["computer-use"]["required_macos"] == "15.0"


def test_browser_use_missing_bundle_reports_missing_bundle(tmp_path: Path):
    report = inspect_codex_plugins(tmp_path)
    assert report["browser-use"]["status"] == STATUS_MISSING_BUNDLE


def test_chrome_missing_extension_reports_user_action_required(tmp_path: Path):
    write_plugin_manifest(tmp_path, "chrome", "0.1.7")
    native_host = tmp_path / "chrome-native" / "com.openai.codexextension.json"
    native_host.parent.mkdir(parents=True, exist_ok=True)
    native_host.write_text(
        '{"allowed_origins":["chrome-extension://hehggadaopoacecdllhhajmbjkdcmajg/"]}',
        encoding="utf-8",
    )
    report = inspect_codex_plugins(tmp_path, native_host_manifest=native_host, chrome_extensions_dir=tmp_path / "extensions")
    assert report["chrome"]["status"] == STATUS_USER_ACTION_REQUIRED


def test_chrome_connected_requires_matching_native_host_and_extension(tmp_path: Path):
    write_plugin_manifest(tmp_path, "chrome", "0.1.7")
    native_host = tmp_path / "chrome-native" / "com.openai.codexextension.json"
    native_host.parent.mkdir(parents=True, exist_ok=True)
    native_host.write_text(
        '{"allowed_origins":["chrome-extension://hehggadaopoacecdllhhajmbjkdcmajg/"]}',
        encoding="utf-8",
    )
    extension_dir = tmp_path / "extensions" / "hehggadaopoacecdllhhajmbjkdcmajg" / "1.1.4_0"
    extension_dir.mkdir(parents=True, exist_ok=True)
    report = inspect_codex_plugins(tmp_path, native_host_manifest=native_host, chrome_extensions_dir=tmp_path / "extensions")
    assert report["chrome"]["status"] == STATUS_CONFIGURED


def test_doctor_uses_windows_chrome_paths_when_requested(monkeypatch, tmp_path: Path):
    monkeypatch.setenv("LOCALAPPDATA", str(tmp_path / "LocalAppData"))
    native_host, extensions = chrome_integration_paths("nt")
    assert native_host is not None and extensions is not None
    assert "LocalAppData" in str(native_host)
