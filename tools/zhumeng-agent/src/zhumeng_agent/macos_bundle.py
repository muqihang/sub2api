from __future__ import annotations

import argparse
import os
import plistlib
import stat
from pathlib import Path

BUNDLE_NAME = "Zhumeng Agent.app"
BUNDLE_EXECUTABLE = "Zhumeng Agent"


def default_repo_root() -> Path:
    return Path(__file__).resolve().parents[4]


def build_macos_app_bundle(
    *,
    repo_root: Path,
    output_root: Path | None = None,
    python_bin: Path | None = None,
) -> Path:
    repo_root = repo_root.resolve()
    tool_root = repo_root / "tools/zhumeng-agent"
    output_root = (output_root or (tool_root / "dist")).resolve()
    bundle_root = output_root / BUNDLE_NAME
    contents_dir = bundle_root / "Contents"
    macos_dir = contents_dir / "MacOS"
    resources_dir = contents_dir / "Resources"
    macos_dir.mkdir(parents=True, exist_ok=True)
    resources_dir.mkdir(parents=True, exist_ok=True)

    template_path = tool_root / "packaging" / "macos" / "Info.plist"
    info = plistlib.loads(template_path.read_bytes())
    info["CFBundleExecutable"] = BUNDLE_EXECUTABLE
    info.setdefault("CFBundlePackageType", "APPL")
    info.setdefault("CFBundleShortVersionString", "0.1.0")
    info.setdefault("CFBundleVersion", "0.1.0")
    (contents_dir / "Info.plist").write_bytes(plistlib.dumps(info, sort_plist=False))

    launcher_path = macos_dir / BUNDLE_EXECUTABLE
    launcher_path.write_text(build_launcher_script(repo_root=repo_root, python_bin=python_bin), encoding="utf-8")
    launcher_mode = launcher_path.stat().st_mode
    launcher_path.chmod(launcher_mode | stat.S_IRUSR | stat.S_IWUSR | stat.S_IXUSR | stat.S_IRGRP | stat.S_IXGRP | stat.S_IROTH | stat.S_IXOTH)

    (contents_dir / "PkgInfo").write_text("APPLZhmg", encoding="utf-8")
    return bundle_root


def build_launcher_script(*, repo_root: Path, python_bin: Path | None = None) -> str:
    repo_root = repo_root.resolve()
    python_path = python_bin.resolve() if python_bin is not None else (repo_root / "tools/zhumeng-agent/.venv/bin/python")
    return f"""#!/bin/sh
set -eu

REPO_ROOT="{repo_root}"
PYTHON_BIN="${{ZHUMENG_AGENT_PYTHON_BIN:-{python_path}}}"

if [ ! -x "$PYTHON_BIN" ]; then
  echo "Zhumeng Agent could not find a runnable Python interpreter at: $PYTHON_BIN" >&2
  exit 1
fi

cd "$REPO_ROOT/tools/zhumeng-agent"
exec "$PYTHON_BIN" -m zhumeng_agent "$@"
"""


def build_from_cli(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="build_app_bundle")
    parser.add_argument("--repo-root", type=Path, default=default_repo_root())
    parser.add_argument("--output-root", type=Path, default=None)
    parser.add_argument("--python-bin", type=Path, default=None)
    args = parser.parse_args(argv)
    bundle_root = build_macos_app_bundle(
        repo_root=args.repo_root,
        output_root=args.output_root,
        python_bin=args.python_bin,
    )
    print(bundle_root)
    return 0
