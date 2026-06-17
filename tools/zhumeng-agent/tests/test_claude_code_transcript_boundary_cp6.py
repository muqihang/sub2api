from __future__ import annotations

import json

from zhumeng_agent.adapters.claude_code.transcript_boundary import (
    ProviderProfile,
    ReplayClass,
    assert_claude_native_replay_safe,
    freeze_safe_summary,
    freeze_safe_tool_result,
    sanitize_for_target_provider,
)


def _profile(provider: str, *, native_replay_allowed: bool = False) -> ProviderProfile:
    route = "claude_native" if provider == "claude" else f"{provider}_bridge"
    client_type = "claude_code_native" if provider == "claude" else f"claude_code_bridge_{provider}"
    return ProviderProfile(
        provider=provider,
        route=route,
        client_type=client_type,
        model_id="claude-sonnet-4-6" if provider == "claude" else f"{provider}-model",
        native_replay_allowed=native_replay_allowed,
    )


def _snapshot(provider: str, messages: list[dict[str, object]]) -> dict[str, object]:
    # Mirrors the sanitizer's stable source hash contract without exposing raw provider-private fields.
    from zhumeng_agent.adapters.claude_code.transcript_boundary import _stable_hash  # type: ignore[attr-defined]  # noqa: PLC0415

    return {
        "conversation_ref": "cp6-fixture",
        "source_message_count": len(messages),
        "source_hash": _stable_hash(messages),
        "turns": [
            {
                "provider": provider,
                "route": f"{provider}_bridge",
                "range": [0, len(messages) - 1],
                "replay_class": ReplayClass.SUMMARY_ONLY.value,
            }
        ],
    }


def test_cp6_mid_tool_loop_cross_provider_switch_blocks_raw_tool_use_blocks():
    source_messages = [
        {
            "role": "assistant",
            "content": [
                {"type": "text", "text": "I will call a tool now."},
                {"type": "tool_use", "id": "toolu_partial", "name": "ToolSearch", "input": {"query": "secret"}},
            ],
        }
    ]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="normal",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "raw_tool_use_block_requires_safe_summary"
    assert "messages[0].content[1].type" in result.blocked_paths


def test_cp6_mid_tool_loop_switch_allows_once_frozen_safe_summary_only():
    frozen = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="Tool loop was summarized safely once.",
        evidence={"tools": ["ToolSearch"], "files": ["b.py", "a.py"]},
    )
    source_messages = [dict(frozen.block)]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="compact",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert result.transcript.messages == tuple(source_messages)
    assert result.transcript.provenance[0].stable_id == frozen.provenance.stable_id
    assert_claude_native_replay_safe(result.transcript)


def test_cp6_safe_tool_result_freezes_stable_hash_and_reuses_idempotently():
    first = freeze_safe_tool_result(
        source_provider="deepseek",
        source_turn_range=(3, 4),
        tool_use_id="toolu_cp6",
        content="Read-only ToolSearch result summary.",
        evidence={"files": ["b.py", "a.py"], "tools": ["ToolSearch"]},
    )
    second = freeze_safe_tool_result(
        source_provider="deepseek",
        source_turn_range=(3, 4),
        tool_use_id="toolu_cp6",
        content="Read-only ToolSearch result summary.",
        evidence={"tools": ["ToolSearch"], "files": ["a.py", "b.py"]},
    )

    assert first.block == second.block
    assert first.provenance == second.provenance
    assert first.provenance.replay_class == ReplayClass.SAFE_TOOL_RESULT
    assert first.block["safe_tool_result"]["stable_id"] == first.provenance.stable_id
    assert first.block["safe_tool_result"]["source_hash"] == first.provenance.source_hash

    result = sanitize_for_target_provider(
        source_messages=[dict(first.block)],
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="history_replay",
        provenance_snapshot=_snapshot("deepseek", [dict(first.block)]),
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert result.transcript.messages == (first.block,)
    assert result.transcript.provenance[0].stable_id == first.provenance.stable_id
    assert_claude_native_replay_safe(result.transcript)


def test_cp6_frozen_safe_tool_result_rejects_whitespace_tamper():
    frozen = freeze_safe_tool_result(
        source_provider="deepseek",
        source_turn_range=(3, 4),
        tool_use_id="toolu_cp6",
        content="Read-only ToolSearch result summary.",
        evidence={"tools": ["ToolSearch"]},
    )
    tampered = dict(frozen.block)
    tool_result = dict(tampered["safe_tool_result"])
    tool_result["content"] = tool_result["content"] + " "
    tampered["safe_tool_result"] = tool_result

    result = sanitize_for_target_provider(
        source_messages=[tampered],
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="history_replay",
        provenance_snapshot=_snapshot("deepseek", [tampered]),
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "invalid_frozen_safe_tool_result"
    assert result.blocked_paths == ("messages[0].safe_tool_result",)


def test_cp6_cross_provider_to_claude_requires_frozen_safe_tool_result():
    source_messages = [
        {
            "role": "tool",
            "safe_tool_result": {
                "tool_use_id": "toolu_legacy",
                "content": "legacy non-frozen summary",
            },
        }
    ]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="normal",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "safe_tool_result_requires_frozen_envelope"
    assert result.blocked_paths == ("messages[0].safe_tool_result",)


def test_cp6_safe_tool_result_evidence_strips_raw_tool_internals():
    frozen = freeze_safe_tool_result(
        source_provider="deepseek",
        source_turn_range=(3, 4),
        tool_use_id="toolu_cp6",
        content="Read-only ToolSearch result summary.",
        evidence={
            "files": ["a.py"],
            "tool_call": {"name": "ToolSearch", "input": {"query": "secret"}},
            "metadata": {"provider_private": "secret"},
            "raw_tool_runner_state": {"stdout": "secret"},
        },
    )

    result = sanitize_for_target_provider(
        source_messages=[dict(frozen.block)],
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="history_replay",
        provenance_snapshot=_snapshot("deepseek", [dict(frozen.block)]),
    )

    assert result.transcript is not None
    serialized = json.dumps(result.transcript.to_dict(), ensure_ascii=True, sort_keys=True)
    assert "a.py" in serialized
    assert "tool_call" not in serialized
    assert "input" not in serialized
    assert "metadata" not in serialized
    assert "raw_tool_runner_state" not in serialized
    assert "secret" not in serialized


def test_cp6_deepseek_anthropic_looking_thinking_signature_never_replays_to_claude_native():
    source_messages = [
        {
            "role": "assistant",
            "content": [
                {"type": "thinking", "thinking": "foreign hidden reasoning", "signature": "foreign-sig"},
                {"type": "text", "text": "visible answer"},
            ],
        }
    ]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="history_replay",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert_claude_native_replay_safe(result.transcript)
    serialized = json.dumps(result.transcript.to_dict(), sort_keys=True)
    assert "visible answer" in serialized
    assert "foreign hidden reasoning" not in serialized
    assert "foreign-sig" not in serialized
    assert "thinking" not in serialized


def test_cp6_tampered_frozen_safe_summary_fails_during_sanitization():
    frozen = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="stable safe summary",
        evidence={"files": ["a.py"]},
    )
    tampered = dict(frozen.block)
    summary = dict(tampered["zhumeng_safe_summary"])
    summary["text"] = "stable safe summary plus drift"
    tampered["zhumeng_safe_summary"] = summary
    source_messages = [tampered]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="compact",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "invalid_frozen_safe_summary"
    assert result.blocked_paths == ("messages[0].zhumeng_safe_summary",)


def test_cp6_raw_tool_result_with_visible_text_fails_closed_not_partial_replay():
    source_messages = [
        {"role": "assistant", "content": "visible text before raw tool result"},
        {"role": "tool", "tool_use_id": "toolu_raw", "content": "raw provider tool result"},
    ]

    result = sanitize_for_target_provider(
        source_messages=source_messages,
        source_profile=_profile("deepseek"),
        target_profile=_profile("claude", native_replay_allowed=True),
        replay_context="normal",
        provenance_snapshot=_snapshot("deepseek", source_messages),
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "raw_tool_result_requires_safe_tool_result"
    assert "messages[1].tool_use_id" in result.blocked_paths
    assert "messages[1].content" in result.blocked_paths
