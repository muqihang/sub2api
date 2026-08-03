from pathlib import Path
import plistlib
import tomllib

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
    assert (default_repo_root() / "tools" / "zhumeng-agent").is_dir()



def test_pyproject_exposes_zhumeng_claude_script():
    path = Path(__file__).resolve().parents[1] / "pyproject.toml"
    data = tomllib.loads(path.read_text(encoding="utf-8"))

    assert data["project"]["scripts"]["zhumeng-claude"] == "zhumeng_agent.cli:zhumeng_claude_main"


def test_root_build_embeds_frontend_after_building_dist():
    root = Path(__file__).resolve().parents[3]
    makefile = (root / "Makefile").read_text(encoding="utf-8")
    backend_makefile = (root / "backend" / "Makefile").read_text(encoding="utf-8")

    build_target = makefile.split("build:", 1)[1].split("\n", 1)[0]
    assert "build-frontend" in build_target
    assert "build-backend" in build_target
    assert build_target.index("build-frontend") < build_target.index("build-backend")
    assert "-tags embed" in backend_makefile


def test_deploy_embed_build_is_the_documented_production_build():
    root = Path(__file__).resolve().parents[3]
    deploy_makefile = (root / "deploy" / "Makefile").read_text(encoding="utf-8")
    readme = (root / "README.md").read_text(encoding="utf-8")

    assert "build-embed" in deploy_makefile
    assert "-tags embed" in deploy_makefile
    assert "go build -tags embed" in readme

