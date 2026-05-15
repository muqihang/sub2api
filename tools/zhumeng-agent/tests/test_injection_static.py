from pathlib import Path


def test_renderer_injection_contains_expected_modules():
    script = (Path(__file__).resolve().parents[1] / "src" / "zhumeng_agent" / "inject" / "renderer_inject.js").read_text(encoding="utf-8")
    for token in (
        "zhumengMenu",
        "pluginEntryUnlock",
        "pluginPermissionPanel",
        "pluginInstallGuide",
        "sessionDelete",
        "healthReporter",
    ):
        assert token in script


def test_renderer_injection_fail_open_guards():
    script = (Path(__file__).resolve().parents[1] / "src" / "zhumeng_agent" / "inject" / "renderer_inject.js").read_text(encoding="utf-8")
    assert "try {" in script
    assert "console.warn" in script
