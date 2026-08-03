
import argparse

from tools import claude_code_lab_capture as capture


def test_build_env_strips_api_key_env_whitespace(monkeypatch, tmp_path):
    monkeypatch.setenv("ZHUMENG_API_KEY", "  sk-test-value\n")
    args = argparse.Namespace(
        config_dir=None,
        client_api_key=None,
        egress_guard=False,
        api_key_env="ZHUMENG_API_KEY",
    )

    env = capture._build_env(args, 43117, tmp_path / "run")

    assert env["ZHUMENG_API_KEY"] == "sk-test-value"

def test_normalize_command_drops_repeated_separator():
    assert capture._normalize_command(["--", "--", "npx", "-y", "-p", "@anthropic-ai/claude-code@2.1.175", "claude"]) == [
        "npx",
        "-y",
        "-p",
        "@anthropic-ai/claude-code@2.1.175",
        "claude",
    ]


def test_normalize_command_keeps_command_without_separator():
    assert capture._normalize_command(["npx", "-y", "claude"]) == ["npx", "-y", "claude"]

