from pathlib import Path
import plistlib

from zhumeng_agent.macos_bundle import default_repo_root


def test_macos_info_plist_contains_protocol_registration():
    path = Path(__file__).resolve().parents[1] / "packaging" / "macos" / "Info.plist"
    data = plistlib.loads(path.read_bytes())
    assert data["CFBundleExecutable"] == "Zhumeng Agent"
    assert data["CFBundlePackageType"] == "APPL"
    url_types = data["CFBundleURLTypes"][0]
    assert "zhumeng-agent" in url_types["CFBundleURLSchemes"]


def test_windows_reg_contains_protocol_registration():
    path = Path(__file__).resolve().parents[1] / "packaging" / "windows" / "zhumeng-agent-protocol.reg"
    text = path.read_text(encoding="utf-8")
    assert "HKEY_CURRENT_USER\\Software\\Classes\\zhumeng-agent" in text


def test_macos_bundle_default_repo_root_resolves_current_checkout():
    expected = Path(__file__).resolve().parents[3]

    assert default_repo_root() == expected
    assert default_repo_root().name == "sub2api-zhumeng-main"
