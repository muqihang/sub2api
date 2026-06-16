from __future__ import annotations

import json
from pathlib import Path

import pytest

from zhumeng_agent.adapters.claude_code.transcript_boundary import (
    ProviderProfile,
    ReplayClass,
    TranscriptBoundaryError,
    assert_claude_native_replay_safe,
    build_cross_provider_subagent_result,
    freeze_safe_summary,
    sanitize_for_target_provider,
)

FIXTURE_DIR = Path(__file__).parent / "fixtures" / "claude_code_cp3b"


def _fixture(name: str) -> dict[str, object]:
    return json.loads((FIXTURE_DIR / name).read_text(encoding="utf-8"))


def _profile(payload: dict[str, object]) -> ProviderProfile:
    return ProviderProfile(
        provider=str(payload["provider"]),
        route=str(payload["route"]),
        client_type=str(payload["client_type"]),
        model_id=str(payload.get("model_id") or ""),
        native_replay_allowed=bool(payload.get("native_replay_allowed", False)),
    )


def test_cp3b_claude_deepseek_claude_boundary_strips_foreign_reasoning_and_signature():
    payload = _fixture("claude_deepseek_claude_boundary.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=str(payload["replay_context"]),
        provenance_snapshot=payload["provenance_snapshot"],
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert_claude_native_replay_safe(result.transcript)
    serialized = json.dumps(result.transcript.to_dict(), ensure_ascii=True, sort_keys=True)
    for key in payload["expected"]["must_not_contain_keys"]:
        assert key not in serialized
    assert "visible DeepSeek answer" in serialized
    assert "internal chain" not in serialized
    assert {item.replay_class for item in result.transcript.provenance} <= {
        ReplayClass.SAFE_FINAL_ANSWER,
        ReplayClass.SAFE_TOOL_RESULT,
        ReplayClass.EVIDENCE_SUMMARY,
        ReplayClass.SUMMARY_ONLY,
    }


def test_cp3b_cross_provider_subagent_result_only_returns_safe_blocks():
    payload = _fixture("subagent_cross_provider_result.json")

    result = build_cross_provider_subagent_result(
        parent_profile=_profile(payload["parent_profile"]),
        child_profile=_profile(payload["child_profile"]),
        child_final_answer=payload["child_final_answer"],
        child_tool_results=payload["child_tool_results"],
        child_evidence=payload["child_evidence"],
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert_claude_native_replay_safe(result.transcript)
    replay_classes = [item.replay_class for item in result.transcript.provenance]
    assert replay_classes == [
        ReplayClass.SAFE_FINAL_ANSWER,
        ReplayClass.SAFE_TOOL_RESULT,
        ReplayClass.EVIDENCE_SUMMARY,
    ]
    serialized = json.dumps(result.transcript.to_dict(), ensure_ascii=True, sort_keys=True)
    assert "safe patch summary" in serialized
    assert "deepseek hidden reasoning" not in serialized
    assert "provider_private_state_ref" not in serialized
    assert "raw_tool_runner_state" not in serialized
    assert "provider_private_state_ref" not in serialized
    assert "raw_provider_response" not in serialized
    assert "raw hidden" not in serialized


def test_cp3b_foreign_thinking_signature_is_fail_closed_if_no_visible_safe_content():
    payload = _fixture("foreign_thinking_signature_blocked.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=str(payload["replay_context"]),
        provenance_snapshot=payload["provenance_snapshot"],
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "no_replay_safe_content"
    assert "messages[0].reasoning_content" in result.blocked_paths
    assert "messages[0].signature" in result.blocked_paths


@pytest.mark.parametrize("context", ["resume", "continue", "compact", "checkpoint", "history_replay"])
def test_cp3b_replay_contexts_require_provenance_snapshot(context: str):
    payload = _fixture("resume_continue_compact_checkpoint_history_replay.json")

    without_snapshot = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=context,
    )
    with_snapshot = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=context,
        provenance_snapshot=payload["provenance_snapshot"],
    )

    assert without_snapshot.transcript is None
    assert without_snapshot.fail_closed_reason == f"provenance_snapshot_required_for_{context}"
    assert with_snapshot.fail_closed_reason is None
    assert with_snapshot.transcript is not None
    assert_claude_native_replay_safe(with_snapshot.transcript)


def test_cp3b_safe_summary_freezes_hashes_deterministically():
    payload = _fixture("cache_determinism_safe_summary.json")

    first = freeze_safe_summary(
        source_provider=str(payload["source_provider"]),
        source_turn_range=tuple(payload["source_turn_range"]),
        text=str(payload["text"]),
        evidence=payload["evidence"],
    )
    second = freeze_safe_summary(
        source_provider=str(payload["source_provider"]),
        source_turn_range=tuple(payload["source_turn_range"]),
        text=str(payload["text"]),
        evidence={"files": ["b.py", "a.py"], "checks": {"pytest": "passed"}},
    )

    assert first == second
    assert first.provenance.stable_id.startswith("safe-summary:sha256:")
    assert first.provenance.source_hash == first.block["zhumeng_safe_summary"]["source_hash"]
    assert "created_at" not in first.block["zhumeng_safe_summary"]
    assert "nonce" not in first.block["zhumeng_safe_summary"]


def test_cp3b_same_provider_deepseek_preserves_provider_local_history():
    payload = _fixture("same_provider_deepseek_preserve.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=str(payload["replay_context"]),
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert result.transcript.messages == tuple(payload["input_messages"])
    serialized = json.dumps(result.transcript.to_dict(), ensure_ascii=True, sort_keys=True)
    assert "deepseek provider-local reasoning" in serialized
    with pytest.raises(TranscriptBoundaryError, match="foreign provider-private content"):
        assert_claude_native_replay_safe(result.transcript)


def test_cp3b_same_provider_claude_native_multi_message_provenance_is_one_to_one():
    claude = ProviderProfile(
        provider="claude",
        route="claude_native",
        client_type="claude_code_native",
        model_id="claude-sonnet-4-6",
        native_replay_allowed=True,
    )
    messages = [
        {"role": "assistant", "content": "first native Claude answer"},
        {"role": "assistant", "content": "second native Claude answer"},
    ]

    result = sanitize_for_target_provider(
        source_messages=messages,
        source_profile=claude,
        target_profile=claude,
        replay_context="normal",
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    assert len(result.transcript.provenance) == len(messages)
    assert_claude_native_replay_safe(result.transcript)


def test_cp3b_public_api_is_exported_from_adapter_package():
    from zhumeng_agent.adapters.claude_code import (  # noqa: PLC0415
        ProviderProfile as ExportedProviderProfile,
        ReplayClass as ExportedReplayClass,
        ReplaySafeAnthropicTranscript as ExportedTranscript,
        TranscriptBoundaryError as ExportedBoundaryError,
        assert_claude_native_replay_safe as exported_assert_safe,
        build_cross_provider_subagent_result as exported_subagent_result,
        freeze_safe_summary as exported_freeze,
        sanitize_for_target_provider as exported_sanitize,
    )
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ProviderProfile,
        ReplayClass,
        ReplaySafeAnthropicTranscript,
        TranscriptBoundaryError,
        assert_claude_native_replay_safe,
        build_cross_provider_subagent_result,
        freeze_safe_summary,
        sanitize_for_target_provider,
    )

    assert ExportedProviderProfile is ProviderProfile
    assert ExportedReplayClass is ReplayClass
    assert ExportedTranscript is ReplaySafeAnthropicTranscript
    assert ExportedBoundaryError is TranscriptBoundaryError
    assert exported_assert_safe is assert_claude_native_replay_safe
    assert exported_subagent_result is build_cross_provider_subagent_result
    assert exported_freeze is freeze_safe_summary
    assert exported_sanitize is sanitize_for_target_provider


def test_cp3b_raw_foreign_tool_result_without_safe_envelope_fails_closed():
    payload = _fixture("invalid_tool_result_fail_closed.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=str(payload["replay_context"]),
        provenance_snapshot=payload["provenance_snapshot"],
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "raw_tool_result_requires_safe_tool_result"
    assert "messages[0].tool_use_id" in result.blocked_paths


def test_cp3b_cross_provider_to_claude_requires_provenance_snapshot_even_normal():
    payload = _fixture("claude_deepseek_claude_boundary.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context="normal",
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "provenance_snapshot_required_for_cross_provider_to_claude"


def test_cp3b_claude_to_deepseek_strips_claude_private_fields():
    payload = _fixture("claude_to_deepseek_strip_private.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context=str(payload["replay_context"]),
    )

    assert result.fail_closed_reason is None
    assert result.transcript is not None
    serialized = json.dumps(result.transcript.to_dict(), ensure_ascii=True, sort_keys=True)
    assert "visible Claude answer" in serialized
    assert "claude hidden thinking" not in serialized
    assert "anthropic-signature" not in serialized
    assert "forbidden-cache-control" not in serialized
    assert {"messages[0].thinking", "messages[0].signature", "messages[0].cch"}.issubset(set(result.blocked_paths))


def test_cp3b_foreign_provider_cannot_forge_claude_native_replay_class():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        TranscriptProvenance,
        _transcript_hash,
    )

    messages = ({"role": "assistant", "content": "plain but foreign"},)
    provenance = (
        TranscriptProvenance(
            stable_id="forged",
            source_provider="deepseek",
            source_route="deepseek_bridge",
            source_turn_range=(1, 1),
            source_hash="sha256:" + "1" * 64,
            replay_class=ReplayClass.CLAUDE_NATIVE_REPLAYABLE,
        ),
    )
    transcript = ReplaySafeAnthropicTranscript(
        messages=messages,
        provenance=provenance,
        replay_context="normal",
        deterministic_hash=_transcript_hash(messages, provenance, "normal"),
    )

    with pytest.raises(TranscriptBoundaryError, match="Claude native replay class requires Claude provenance"):
        assert_claude_native_replay_safe(transcript)


def test_cp3b_claude_origin_native_thinking_signature_remains_replayable():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        TranscriptProvenance,
        _stable_hash,
        _transcript_hash,
    )

    messages = (
        {
            "role": "assistant",
            "content": "visible Claude answer",
            "thinking": {"text": "signed native thinking", "signature": "anthropic-native-signature"},
        },
    )
    provenance = (
        TranscriptProvenance(
            stable_id="claude-native-turn",
            source_provider="claude",
            source_route="claude_native",
            source_turn_range=(3, 3),
            source_hash=_stable_hash(messages[0]),
            replay_class=ReplayClass.CLAUDE_NATIVE_REPLAYABLE,
        ),
    )
    transcript = ReplaySafeAnthropicTranscript(
        messages=messages,
        provenance=provenance,
        replay_context="normal",
        deterministic_hash=_transcript_hash(messages, provenance, "normal"),
    )

    assert_claude_native_replay_safe(transcript)


def test_cp3b_same_provider_hash_includes_provider_local_reasoning():
    payload = _fixture("same_provider_deepseek_preserve.json")
    source = _profile(payload["source_profile"])
    target = _profile(payload["target_profile"])
    first = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=source,
        target_profile=target,
        replay_context="normal",
    )
    changed_messages = [dict(payload["input_messages"][0], reasoning_content="different provider-local reasoning")]
    second = sanitize_for_target_provider(
        source_messages=changed_messages,
        source_profile=source,
        target_profile=target,
        replay_context="normal",
    )

    assert first.transcript is not None
    assert second.transcript is not None
    assert first.transcript.deterministic_hash != second.transcript.deterministic_hash


def test_cp3b_bogus_provenance_snapshot_fails_closed():
    payload = _fixture("claude_deepseek_claude_boundary.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context="normal",
        provenance_snapshot={"bogus": "yes"},
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "invalid_provenance_snapshot"


def test_cp3b_mismatched_provenance_snapshot_provider_fails_closed():
    payload = _fixture("claude_deepseek_claude_boundary.json")
    snapshot = {"conversation_ref": "cp3b-fixture", "turns": [{"provider": "openai", "route": "openai_bridge", "range": [2, 3]}]}

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context="normal",
        provenance_snapshot=snapshot,
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "invalid_provenance_snapshot"


def test_cp3b_malformed_safe_tool_result_fails_closed():
    payload = _fixture("claude_deepseek_claude_boundary.json")
    messages = [{"role": "tool", "safe_tool_result": {"tool_use_id": "", "content": ""}}]

    from zhumeng_agent.adapters.claude_code.transcript_boundary import _stable_hash  # noqa: PLC0415

    snapshot = {
        **payload["provenance_snapshot"],
        "source_hash": _stable_hash(messages),
        "source_message_count": len(messages),
    }
    result = sanitize_for_target_provider(
        source_messages=messages,
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context="normal",
        provenance_snapshot=snapshot,
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "no_replay_safe_content"
    assert "messages[0].safe_tool_result" in result.blocked_paths


def test_cp3b_stale_provenance_snapshot_source_hash_fails_closed():
    payload = _fixture("claude_deepseek_claude_boundary.json")
    snapshot = dict(payload["provenance_snapshot"])
    snapshot["source_hash"] = "sha256:" + "0" * 64

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=_profile(payload["target_profile"]),
        replay_context="normal",
        provenance_snapshot=snapshot,
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "invalid_provenance_snapshot"


def test_cp3b_replay_safe_verifier_rejects_missing_or_forged_provenance():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        TranscriptProvenance,
    )

    message = {"role": "assistant", "content": "safe answer"}
    missing = ReplaySafeAnthropicTranscript(
        messages=(message,),
        provenance=(),
        replay_context="normal",
        deterministic_hash="sha256:" + "5" * 64,
    )
    forged_provenance = (
        TranscriptProvenance(
            stable_id="safe_final_answer:sha256:" + "6" * 64,
            source_provider="deepseek",
            source_route="deepseek_bridge",
            source_turn_range=(0, 0),
            source_hash="sha256:" + "6" * 64,
            replay_class=ReplayClass.SAFE_FINAL_ANSWER,
        ),
    )
    from zhumeng_agent.adapters.claude_code.transcript_boundary import _transcript_hash  # noqa: PLC0415
    forged = ReplaySafeAnthropicTranscript(
        messages=(message,),
        provenance=forged_provenance,
        replay_context="normal",
        deterministic_hash=_transcript_hash((message,), forged_provenance, "normal"),
    )

    with pytest.raises(TranscriptBoundaryError, match="message/provenance cardinality"):
        assert_claude_native_replay_safe(missing)
    with pytest.raises(TranscriptBoundaryError, match="provenance hash mismatch"):
        assert_claude_native_replay_safe(forged)


def test_cp3b_replay_safe_verifier_rejects_deterministic_hash_drift():
    result = build_cross_provider_subagent_result(
        parent_profile=ProviderProfile("claude", "claude_native", "claude_code_native", native_replay_allowed=True),
        child_profile=ProviderProfile("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
        child_final_answer={"content": "safe final answer"},
        child_tool_results=[],
        child_evidence={},
    )
    assert result.transcript is not None
    from zhumeng_agent.adapters.claude_code.transcript_boundary import ReplaySafeAnthropicTranscript  # noqa: PLC0415

    drifted = ReplaySafeAnthropicTranscript(
        messages=result.transcript.messages,
        provenance=result.transcript.provenance,
        replay_context=result.transcript.replay_context,
        deterministic_hash="sha256:" + "8" * 64,
    )

    with pytest.raises(TranscriptBoundaryError, match="deterministic hash mismatch"):
        assert_claude_native_replay_safe(drifted)

def test_cp3b_cross_provider_to_claude_fails_closed_even_when_native_replay_disabled_by_default():
    result = sanitize_for_target_provider(
        source_messages=[{"role": "assistant", "content": "visible"}],
        source_profile=ProviderProfile("deepseek", "deepseek_bridge", "claude_code_bridge_deepseek"),
        target_profile=ProviderProfile("claude", "claude_native", "claude_code_native"),
        replay_context="normal",
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "provenance_snapshot_required_for_cross_provider_to_claude"
    assert result.evidence_summary["mode"] == "fail_closed"


def test_cp3b_cross_provider_to_claude_with_valid_snapshot_but_native_disabled_fails_closed():
    payload = _fixture("claude_deepseek_claude_boundary.json")

    result = sanitize_for_target_provider(
        source_messages=payload["input_messages"],
        source_profile=_profile(payload["source_profile"]),
        target_profile=ProviderProfile("claude", "claude_native", "claude_code_native"),
        replay_context="normal",
        provenance_snapshot=payload["provenance_snapshot"],
    )

    assert result.transcript is None
    assert result.fail_closed_reason == "claude_native_replay_not_allowed"
    assert result.blocked_paths == ("claude_native_replay_allowed",)


def test_cp3b_frozen_safe_summary_can_be_replayed_to_claude_native():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        _transcript_hash,
    )

    summary = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="safe summary",
        evidence={"files": ["a.py"]},
    )
    transcript = ReplaySafeAnthropicTranscript(
        messages=(summary.block,),
        provenance=(summary.provenance,),
        replay_context="normal",
        deterministic_hash=_transcript_hash((summary.block,), (summary.provenance,), "normal"),
    )

    assert_claude_native_replay_safe(transcript)


def test_cp3b_frozen_safe_summary_rejects_text_tamper_even_with_recomputed_transcript_hash():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        _transcript_hash,
    )

    summary = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="safe summary",
        evidence={"files": ["a.py"]},
    )
    tampered_summary = dict(summary.block["zhumeng_safe_summary"])
    tampered_summary["text"] = "tampered summary"
    tampered_block = {"role": "assistant", "zhumeng_safe_summary": tampered_summary}
    transcript = ReplaySafeAnthropicTranscript(
        messages=(tampered_block,),
        provenance=(summary.provenance,),
        replay_context="normal",
        deterministic_hash=_transcript_hash((tampered_block,), (summary.provenance,), "normal"),
    )

    with pytest.raises(TranscriptBoundaryError, match="provenance hash mismatch"):
        assert_claude_native_replay_safe(transcript)


def test_cp3b_frozen_safe_summary_rejects_role_or_extra_key_tamper():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        _transcript_hash,
    )

    summary = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="safe summary",
        evidence={"files": ["a.py"]},
    )
    mutated_blocks = [
        {"role": "user", "zhumeng_safe_summary": summary.block["zhumeng_safe_summary"]},
        {"role": "assistant", "zhumeng_safe_summary": summary.block["zhumeng_safe_summary"], "content": "extra"},
        {
            "role": "assistant",
            "zhumeng_safe_summary": {**summary.block["zhumeng_safe_summary"], "extra": "not frozen"},
        },
    ]

    for block in mutated_blocks:
        transcript = ReplaySafeAnthropicTranscript(
            messages=(block,),
            provenance=(summary.provenance,),
            replay_context="normal",
            deterministic_hash=_transcript_hash((block,), (summary.provenance,), "normal"),
        )
        with pytest.raises(TranscriptBoundaryError, match="provenance hash mismatch"):
            assert_claude_native_replay_safe(transcript)


def test_cp3b_frozen_safe_summary_rejects_evidence_type_tamper():
    from zhumeng_agent.adapters.claude_code.transcript_boundary import (  # noqa: PLC0415
        ReplaySafeAnthropicTranscript,
        _transcript_hash,
    )

    summary = freeze_safe_summary(
        source_provider="deepseek",
        source_turn_range=(0, 0),
        text="safe summary",
        evidence={},
    )
    tampered_summary = dict(summary.block["zhumeng_safe_summary"])
    tampered_summary["evidence"] = []
    tampered_block = {"role": "assistant", "zhumeng_safe_summary": tampered_summary}
    transcript = ReplaySafeAnthropicTranscript(
        messages=(tampered_block,),
        provenance=(summary.provenance,),
        replay_context="normal",
        deterministic_hash=_transcript_hash((tampered_block,), (summary.provenance,), "normal"),
    )

    with pytest.raises(TranscriptBoundaryError, match="provenance hash mismatch"):
        assert_claude_native_replay_safe(transcript)
