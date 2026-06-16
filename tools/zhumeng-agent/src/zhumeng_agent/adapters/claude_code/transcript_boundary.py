from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from enum import StrEnum
from typing import Mapping, Sequence


class TranscriptBoundaryError(RuntimeError):
    pass


class ReplayClass(StrEnum):
    CLAUDE_NATIVE_REPLAYABLE = "claude_native_replayable"
    BRIDGE_LOCAL_ONLY = "bridge_local_only"
    SUMMARY_ONLY = "summary_only"
    SAFE_FINAL_ANSWER = "safe_final_answer"
    SAFE_TOOL_RESULT = "safe_tool_result"
    EVIDENCE_SUMMARY = "evidence_summary"


@dataclass(frozen=True, slots=True)
class ProviderProfile:
    provider: str
    route: str
    client_type: str
    model_id: str = ""
    native_replay_allowed: bool = False


@dataclass(frozen=True, slots=True)
class TranscriptProvenance:
    stable_id: str
    source_provider: str
    source_route: str
    source_turn_range: tuple[int, int]
    source_hash: str
    replay_class: ReplayClass

    def to_dict(self) -> dict[str, object]:
        return {
            "stable_id": self.stable_id,
            "source_provider": self.source_provider,
            "source_route": self.source_route,
            "source_turn_range": list(self.source_turn_range),
            "source_hash": self.source_hash,
            "replay_class": self.replay_class.value,
        }


@dataclass(frozen=True, slots=True)
class ReplaySafeAnthropicTranscript:
    messages: tuple[Mapping[str, object], ...]
    provenance: tuple[TranscriptProvenance, ...]
    replay_context: str
    deterministic_hash: str

    def to_dict(self) -> dict[str, object]:
        return {
            "messages": list(self.messages),
            "provenance": [item.to_dict() for item in self.provenance],
            "replay_context": self.replay_context,
            "deterministic_hash": self.deterministic_hash,
        }


@dataclass(frozen=True, slots=True)
class BoundaryResult:
    transcript: ReplaySafeAnthropicTranscript | None
    blocked_paths: tuple[str, ...]
    evidence_summary: Mapping[str, object]
    fail_closed_reason: str | None = None


@dataclass(frozen=True, slots=True)
class FrozenSafeSummary:
    block: Mapping[str, object]
    provenance: TranscriptProvenance


_FORBIDDEN_FOREIGN_KEYS = frozenset(
    {
        "thinking",
        "reasoning",
        "reasoning_content",
        "signature",
        "thoughtSignature",
        "provider_private_state_ref",
        "raw_tool_runner_state",
        "previous_response_id",
        "raw_provider_request",
        "raw_provider_response",
        "cch",
    }
)
_REPLAY_CONTEXTS_REQUIRING_PROVENANCE = frozenset({"resume", "continue", "compact", "checkpoint", "history_replay"})
_CLAUDE_SAFE_REPLAY_CLASSES = frozenset(
    {
        ReplayClass.CLAUDE_NATIVE_REPLAYABLE,
        ReplayClass.SUMMARY_ONLY,
        ReplayClass.SAFE_FINAL_ANSWER,
        ReplayClass.SAFE_TOOL_RESULT,
        ReplayClass.EVIDENCE_SUMMARY,
    }
)


def sanitize_for_target_provider(
    *,
    source_messages: Sequence[Mapping[str, object]],
    source_profile: ProviderProfile,
    target_profile: ProviderProfile,
    replay_context: str = "normal",
    provenance_snapshot: Mapping[str, object] | None = None,
) -> BoundaryResult:
    if source_profile.provider == target_profile.provider:
        replay_class = ReplayClass.BRIDGE_LOCAL_ONLY if source_profile.provider != "claude" else ReplayClass.CLAUDE_NATIVE_REPLAYABLE
        provenance = tuple(
            TranscriptProvenance(
                stable_id=_stable_id("same-provider", source_profile.provider, {"index": index, "message": message}),
                source_provider=source_profile.provider,
                source_route=source_profile.route,
                source_turn_range=(index, index),
                source_hash=_stable_hash(message),
                replay_class=replay_class,
            )
            for index, message in enumerate(source_messages)
        )
        transcript = _build_transcript(
            messages=tuple(source_messages),
            provenance=provenance,
            replay_context=replay_context,
        )
        return BoundaryResult(transcript=transcript, blocked_paths=(), evidence_summary={"mode": "same_provider_preserve"})

    if replay_context in _REPLAY_CONTEXTS_REQUIRING_PROVENANCE and not provenance_snapshot:
        return BoundaryResult(
            transcript=None,
            blocked_paths=("provenance_snapshot",),
            evidence_summary={"mode": "fail_closed", "target_provider": target_profile.provider},
            fail_closed_reason=f"provenance_snapshot_required_for_{replay_context}",
        )

    if target_profile.provider == "claude":
        if not provenance_snapshot:
            return BoundaryResult(
                transcript=None,
                blocked_paths=("provenance_snapshot",),
                evidence_summary={
                    "mode": "fail_closed",
                    "source_provider": source_profile.provider,
                    "target_provider": target_profile.provider,
                },
                fail_closed_reason="provenance_snapshot_required_for_cross_provider_to_claude",
            )
        if not _valid_provenance_snapshot(
            provenance_snapshot,
            source_profile=source_profile,
            source_messages=source_messages,
            source_count=len(source_messages),
        ):
            return BoundaryResult(
                transcript=None,
                blocked_paths=("provenance_snapshot",),
                evidence_summary={
                    "mode": "fail_closed",
                    "source_provider": source_profile.provider,
                    "target_provider": target_profile.provider,
                },
                fail_closed_reason="invalid_provenance_snapshot",
            )
        if not target_profile.native_replay_allowed:
            return BoundaryResult(
                transcript=None,
                blocked_paths=("claude_native_replay_allowed",),
                evidence_summary={
                    "mode": "fail_closed",
                    "source_provider": source_profile.provider,
                    "target_provider": target_profile.provider,
                },
                fail_closed_reason="claude_native_replay_not_allowed",
            )
        return _sanitize_into_claude(
            source_messages=source_messages,
            source_profile=source_profile,
            replay_context=replay_context,
            provenance_snapshot=provenance_snapshot,
        )

    return _sanitize_out_of_claude(
        source_messages=source_messages,
        source_profile=source_profile,
        target_profile=target_profile,
        replay_context=replay_context,
    )


def build_cross_provider_subagent_result(
    *,
    parent_profile: ProviderProfile,
    child_profile: ProviderProfile,
    child_final_answer: object,
    child_tool_results: Sequence[Mapping[str, object]],
    child_evidence: Mapping[str, object],
) -> BoundaryResult:
    if parent_profile.provider == child_profile.provider:
        replay_class = ReplayClass.BRIDGE_LOCAL_ONLY if parent_profile.provider != "claude" else ReplayClass.CLAUDE_NATIVE_REPLAYABLE
    else:
        replay_class = ReplayClass.SAFE_FINAL_ANSWER
    messages: list[Mapping[str, object]] = []
    provenance: list[TranscriptProvenance] = []

    final_text = _visible_text(child_final_answer)
    if final_text:
        block = {"role": "assistant", "content": final_text}
        messages.append(block)
        provenance.append(_provenance_for_block(block, child_profile, (0, 0), replay_class))

    for index, tool_result in enumerate(child_tool_results):
        summary = str(tool_result.get("summary") or tool_result.get("content") or "").strip()
        if not summary:
            continue
        block = {
            "role": "tool",
            "safe_tool_result": {
                "tool_use_id": str(tool_result.get("tool_use_id") or f"subagent_tool_{index}"),
                "content": summary,
            },
        }
        messages.append(block)
        provenance.append(_provenance_for_block(block, child_profile, (index + 1, index + 1), ReplayClass.SAFE_TOOL_RESULT))

    evidence_block = _canonicalize_evidence(child_evidence)
    if evidence_block:
        block = {"role": "assistant", "evidence_summary": evidence_block}
        messages.append(block)
        provenance.append(_provenance_for_block(block, child_profile, (len(child_tool_results) + 1, len(child_tool_results) + 1), ReplayClass.EVIDENCE_SUMMARY))

    if not messages:
        return BoundaryResult(
            transcript=None,
            blocked_paths=("child_result",),
            evidence_summary={"parent_provider": parent_profile.provider, "child_provider": child_profile.provider},
            fail_closed_reason="no_replay_safe_content",
        )
    transcript = _build_transcript(messages=tuple(messages), provenance=tuple(provenance), replay_context="subagent_result")
    return BoundaryResult(
        transcript=transcript,
        blocked_paths=(),
        evidence_summary={"parent_provider": parent_profile.provider, "child_provider": child_profile.provider},
    )


def freeze_safe_summary(
    *,
    source_provider: str,
    source_turn_range: tuple[int, int],
    text: str,
    evidence: Mapping[str, object],
) -> FrozenSafeSummary:
    canonical_evidence = _canonicalize_evidence(evidence)
    body = {
        "evidence": canonical_evidence,
        "source_provider": source_provider,
        "source_turn_range": list(source_turn_range),
        "text": text,
    }
    source_hash = _safe_hash(body)
    stable_id = "safe-summary:" + source_hash
    block = {
        "role": "assistant",
        "zhumeng_safe_summary": {
            "stable_id": stable_id,
            "source_provider": source_provider,
            "source_turn_range": list(source_turn_range),
            "source_hash": source_hash,
            "text": text,
            "evidence": canonical_evidence,
        },
    }
    provenance = TranscriptProvenance(
        stable_id=stable_id,
        source_provider=source_provider,
        source_route=f"{source_provider}_bridge" if source_provider != "claude" else "claude_native",
        source_turn_range=source_turn_range,
        source_hash=source_hash,
        replay_class=ReplayClass.SUMMARY_ONLY,
    )
    return FrozenSafeSummary(block=block, provenance=provenance)


def assert_claude_native_replay_safe(transcript: ReplaySafeAnthropicTranscript) -> None:
    if len(transcript.messages) != len(transcript.provenance):
        raise TranscriptBoundaryError("message/provenance cardinality mismatch")
    expected_hash = _transcript_hash(transcript.messages, transcript.provenance, transcript.replay_context)
    if transcript.deterministic_hash != expected_hash:
        raise TranscriptBoundaryError("deterministic hash mismatch")
    provenance_by_index = _provenance_by_message_index(transcript.provenance)
    for index, item in enumerate(transcript.provenance):
        if item.replay_class not in _CLAUDE_SAFE_REPLAY_CLASSES:
            raise TranscriptBoundaryError("foreign provider-private content is not replay-safe for Claude native")
        if item.replay_class == ReplayClass.CLAUDE_NATIVE_REPLAYABLE and item.source_provider != "claude":
            raise TranscriptBoundaryError("Claude native replay class requires Claude provenance")
        message = transcript.messages[index]
        if item.replay_class != ReplayClass.CLAUDE_NATIVE_REPLAYABLE:
            if _frozen_safe_summary_provenance_matches(message, item):
                continue
            expected_source_hash = _safe_hash(message)
            expected_stable_id = f"{item.replay_class.value}:{expected_source_hash}"
            if item.source_hash != expected_source_hash or item.stable_id != expected_stable_id:
                raise TranscriptBoundaryError("provenance hash mismatch")
    for index, message in enumerate(transcript.messages):
        item = provenance_by_index.get(index)
        if item is not None and item.replay_class == ReplayClass.CLAUDE_NATIVE_REPLAYABLE and item.source_provider == "claude":
            continue
        blocked = _find_forbidden_paths(message, prefix=f"messages[{index}]")
        if blocked:
            raise TranscriptBoundaryError("foreign reasoning/signature/tool internals are not replay-safe for Claude native")


def _provenance_by_message_index(provenance: tuple[TranscriptProvenance, ...]) -> dict[int, TranscriptProvenance]:
    by_index: dict[int, TranscriptProvenance] = {}
    for index, item in enumerate(provenance):
        by_index[index] = item
    return by_index


def _frozen_safe_summary_provenance_matches(message: Mapping[str, object], item: TranscriptProvenance) -> bool:
    if item.replay_class != ReplayClass.SUMMARY_ONLY or not _valid_frozen_safe_summary_block(message):
        return False
    summary = message["zhumeng_safe_summary"]
    if not isinstance(summary, Mapping):
        return False
    source_hash = str(summary.get("source_hash") or "")
    stable_id = str(summary.get("stable_id") or "")
    if item.source_hash != source_hash or item.stable_id != stable_id:
        return False
    source_provider = str(summary.get("source_provider") or "")
    if source_provider != item.source_provider:
        return False
    turn_range = summary.get("source_turn_range")
    if not isinstance(turn_range, Sequence) or isinstance(turn_range, (str, bytes)) or len(turn_range) != 2:
        return False
    try:
        normalized_turn_range = tuple(int(value) for value in turn_range)
    except (TypeError, ValueError):
        return False
    return normalized_turn_range == item.source_turn_range


def _valid_frozen_safe_summary_block(message: Mapping[str, object]) -> bool:
    summary = message.get("zhumeng_safe_summary")
    if not isinstance(summary, Mapping):
        return False
    if set(str(key) for key in message) != {"role", "zhumeng_safe_summary"} or message.get("role") != "assistant":
        return False
    if set(str(key) for key in summary) != {
        "stable_id",
        "source_provider",
        "source_turn_range",
        "source_hash",
        "text",
        "evidence",
    }:
        return False
    source_hash = str(summary.get("source_hash") or "")
    stable_id = str(summary.get("stable_id") or "")
    if not source_hash.startswith("sha256:") or stable_id != f"safe-summary:{source_hash}":
        return False
    source_provider = str(summary.get("source_provider") or "")
    turn_range = summary.get("source_turn_range")
    if not source_provider or not isinstance(turn_range, Sequence) or isinstance(turn_range, (str, bytes)) or len(turn_range) != 2:
        return False
    try:
        normalized_turn_range = tuple(int(value) for value in turn_range)
    except (TypeError, ValueError):
        return False
    evidence = summary.get("evidence")
    if not isinstance(evidence, Mapping):
        return False
    body = {
        "evidence": evidence,
        "source_provider": source_provider,
        "source_turn_range": list(normalized_turn_range),
        "text": str(summary.get("text") or ""),
    }
    return source_hash == _safe_hash(body)


def _sanitize_into_claude(
    *,
    source_messages: Sequence[Mapping[str, object]],
    source_profile: ProviderProfile,
    replay_context: str,
    provenance_snapshot: Mapping[str, object] | None,
) -> BoundaryResult:
    messages: list[Mapping[str, object]] = []
    provenance: list[TranscriptProvenance] = []
    blocked: list[str] = []
    for index, message in enumerate(source_messages):
        blocked.extend(_find_forbidden_paths(message, prefix=f"messages[{index}]"))
    raw_tool_use_paths = tuple(path for path in blocked if path.endswith(".type"))
    if raw_tool_use_paths:
        return BoundaryResult(
            transcript=None,
            blocked_paths=raw_tool_use_paths,
            evidence_summary={
                "mode": "fail_closed",
                "source_provider": source_profile.provider,
                "target_provider": "claude",
            },
            fail_closed_reason="raw_tool_use_block_requires_safe_summary",
        )
    raw_tool_result_paths = tuple(
        path for path in blocked
        if ".tool_use_id" in path or (path.endswith(".content") and "safe_tool_result" not in path)
    )
    if raw_tool_result_paths:
        return BoundaryResult(
            transcript=None,
            blocked_paths=raw_tool_result_paths,
            evidence_summary={
                "mode": "fail_closed",
                "source_provider": source_profile.provider,
                "target_provider": "claude",
            },
            fail_closed_reason="raw_tool_result_requires_safe_tool_result",
        )
    invalid_summary_paths = tuple(
        f"messages[{index}].zhumeng_safe_summary"
        for index, message in enumerate(source_messages)
        if "zhumeng_safe_summary" in message and not _valid_frozen_safe_summary_block(message)
    )
    if invalid_summary_paths:
        return BoundaryResult(
            transcript=None,
            blocked_paths=invalid_summary_paths,
            evidence_summary={
                "mode": "fail_closed",
                "source_provider": source_profile.provider,
                "target_provider": "claude",
            },
            fail_closed_reason="invalid_frozen_safe_summary",
        )
    for index, message in enumerate(source_messages):
        safe_block, replay_class = _safe_claude_block(message)
        if safe_block is None:
            continue
        messages.append(safe_block)
        provenance.append(_provenance_for_block(safe_block, source_profile, (index, index), replay_class))
    if not messages:
        return BoundaryResult(
            transcript=None,
            blocked_paths=tuple(blocked),
            evidence_summary={
                "mode": "cross_provider_to_claude",
                "source_provider": source_profile.provider,
                "provenance_snapshot_present": provenance_snapshot is not None,
            },
            fail_closed_reason="no_replay_safe_content",
        )
    transcript = _build_transcript(messages=tuple(messages), provenance=tuple(provenance), replay_context=replay_context)
    return BoundaryResult(
        transcript=transcript,
        blocked_paths=tuple(blocked),
        evidence_summary={
            "mode": "cross_provider_to_claude",
            "source_provider": source_profile.provider,
            "provenance_snapshot_present": provenance_snapshot is not None,
        },
    )


def _sanitize_out_of_claude(
    *,
    source_messages: Sequence[Mapping[str, object]],
    source_profile: ProviderProfile,
    target_profile: ProviderProfile,
    replay_context: str,
) -> BoundaryResult:
    messages: list[Mapping[str, object]] = []
    provenance: list[TranscriptProvenance] = []
    blocked: list[str] = []
    for index, message in enumerate(source_messages):
        blocked.extend(_find_forbidden_paths(message, prefix=f"messages[{index}]"))
        safe_block, replay_class = _safe_claude_block(message)
        if safe_block is None:
            continue
        messages.append(safe_block)
        provenance.append(_provenance_for_block(safe_block, source_profile, (index, index), replay_class))
    if not messages:
        return BoundaryResult(
            transcript=None,
            blocked_paths=tuple(blocked),
            evidence_summary={"mode": "cross_provider_from_claude", "target_provider": target_profile.provider},
            fail_closed_reason="no_replay_safe_content",
        )
    return BoundaryResult(
        transcript=_build_transcript(messages=tuple(messages), provenance=tuple(provenance), replay_context=replay_context),
        blocked_paths=tuple(blocked),
        evidence_summary={"mode": "cross_provider_from_claude", "target_provider": target_profile.provider},
    )


def _safe_claude_block(message: Mapping[str, object]) -> tuple[Mapping[str, object] | None, ReplayClass]:
    if "zhumeng_safe_summary" in message and isinstance(message["zhumeng_safe_summary"], Mapping):
        if not _valid_frozen_safe_summary_block(message):
            return None, ReplayClass.BRIDGE_LOCAL_ONLY
        return {"role": "assistant", "zhumeng_safe_summary": _canonicalize_evidence(message["zhumeng_safe_summary"])}, ReplayClass.SUMMARY_ONLY
    if "safe_tool_result" in message and isinstance(message["safe_tool_result"], Mapping):
        tool_result = message["safe_tool_result"]
        tool_use_id = str(tool_result.get("tool_use_id") or "").strip()
        content = str(tool_result.get("content") or tool_result.get("summary") or "").strip()
        if not tool_use_id or not content:
            return None, ReplayClass.BRIDGE_LOCAL_ONLY
        return {
            "role": "tool",
            "safe_tool_result": {
                "tool_use_id": tool_use_id,
                "content": content,
            },
        }, ReplayClass.SAFE_TOOL_RESULT
    if str(message.get("role") or "") == "tool":
        return None, ReplayClass.BRIDGE_LOCAL_ONLY
    if "evidence_summary" in message and isinstance(message["evidence_summary"], Mapping):
        return {"role": "assistant", "evidence_summary": _canonicalize_evidence(message["evidence_summary"])}, ReplayClass.EVIDENCE_SUMMARY
    text = _visible_text(message)
    if text:
        return {"role": str(message.get("role") or "assistant"), "content": text}, ReplayClass.SAFE_FINAL_ANSWER
    return None, ReplayClass.BRIDGE_LOCAL_ONLY


def _visible_text(value: object) -> str:
    if isinstance(value, str):
        return value.strip()
    if isinstance(value, Mapping):
        content = value.get("content")
        if isinstance(content, str):
            return content.strip()
        if isinstance(content, Sequence) and not isinstance(content, (str, bytes)):
            parts: list[str] = []
            for item in content:
                if isinstance(item, Mapping) and item.get("type") == "text" and isinstance(item.get("text"), str):
                    parts.append(item["text"])
            return "\n".join(parts).strip()
    return ""


def _find_forbidden_paths(value: object, *, prefix: str) -> list[str]:
    paths: list[str] = []
    if isinstance(value, Mapping):
        if value.get("type") == "tool_use":
            paths.append(f"{prefix}.type")
        for key, nested in value.items():
            key_text = str(key)
            path = f"{prefix}.{key_text}"
            if (
                key_text in _FORBIDDEN_FOREIGN_KEYS
                or (value.get("role") == "tool" and key_text in {"tool_use_id", "content"} and "safe_tool_result" not in value)
                or (key_text == "safe_tool_result" and not _valid_safe_tool_result(nested))
            ):
                paths.append(path)
            paths.extend(_find_forbidden_paths(nested, prefix=path))
    elif isinstance(value, Sequence) and not isinstance(value, (str, bytes)):
        for index, nested in enumerate(value):
            paths.extend(_find_forbidden_paths(nested, prefix=f"{prefix}[{index}]"))
    return paths


def _valid_safe_tool_result(value: object) -> bool:
    if not isinstance(value, Mapping):
        return False
    tool_use_id = str(value.get("tool_use_id") or "").strip()
    content = str(value.get("content") or value.get("summary") or "").strip()
    return bool(tool_use_id and content)


def _valid_provenance_snapshot(
    snapshot: Mapping[str, object],
    *,
    source_profile: ProviderProfile,
    source_messages: Sequence[Mapping[str, object]],
    source_count: int,
) -> bool:
    conversation_ref = str(snapshot.get("conversation_ref") or "").strip()
    turns = snapshot.get("turns")
    if not conversation_ref or not isinstance(turns, Sequence) or isinstance(turns, (str, bytes)) or not turns:
        return False
    if int(snapshot.get("source_message_count") or -1) != source_count:
        return False
    expected_source_hash = _stable_hash(source_messages)
    snapshot_source_hash = str(snapshot.get("source_hash") or "")
    if snapshot_source_hash != expected_source_hash:
        return False
    matched = False
    for item in turns:
        if not isinstance(item, Mapping):
            return False
        if str(item.get("provider") or "") != source_profile.provider:
            return False
        route = str(item.get("route") or source_profile.route)
        if route != source_profile.route:
            return False
        turn_range = item.get("range")
        if not isinstance(turn_range, Sequence) or isinstance(turn_range, (str, bytes)) or len(turn_range) != 2:
            return False
        try:
            start = int(turn_range[0])
            end = int(turn_range[1])
        except (TypeError, ValueError):
            return False
        if start < 0 or end < start:
            return False
        replay_class = item.get("replay_class")
        if replay_class is not None and str(replay_class) not in {member.value for member in ReplayClass}:
            return False
        matched = True
    return matched


def _provenance_for_block(
    block: Mapping[str, object],
    profile: ProviderProfile,
    source_turn_range: tuple[int, int],
    replay_class: ReplayClass,
) -> TranscriptProvenance:
    if replay_class == ReplayClass.SUMMARY_ONLY:
        summary = block.get("zhumeng_safe_summary")
        if isinstance(summary, Mapping):
            stable_id = str(summary.get("stable_id") or "")
            source_hash = str(summary.get("source_hash") or "")
            source_provider = str(summary.get("source_provider") or profile.provider)
            turn_range = summary.get("source_turn_range")
            if stable_id.startswith("safe-summary:sha256:") and source_hash.startswith("sha256:") and isinstance(turn_range, Sequence) and not isinstance(turn_range, (str, bytes)) and len(turn_range) == 2:
                try:
                    normalized_turn_range = tuple(int(value) for value in turn_range)
                except (TypeError, ValueError):
                    normalized_turn_range = source_turn_range
                return TranscriptProvenance(
                    stable_id=stable_id,
                    source_provider=source_provider,
                    source_route=f"{source_provider}_bridge" if source_provider != "claude" else "claude_native",
                    source_turn_range=normalized_turn_range,
                    source_hash=source_hash,
                    replay_class=replay_class,
                )
    source_hash = _safe_hash(block)
    return TranscriptProvenance(
        stable_id=f"{replay_class.value}:{source_hash}",
        source_provider=profile.provider,
        source_route=profile.route,
        source_turn_range=source_turn_range,
        source_hash=source_hash,
        replay_class=replay_class,
    )


def _build_transcript(
    *,
    messages: tuple[Mapping[str, object], ...],
    provenance: tuple[TranscriptProvenance, ...],
    replay_context: str,
) -> ReplaySafeAnthropicTranscript:
    return ReplaySafeAnthropicTranscript(
        messages=messages,
        provenance=provenance,
        replay_context=replay_context,
        deterministic_hash=_transcript_hash(messages, provenance, replay_context),
    )


def _transcript_hash(
    messages: tuple[Mapping[str, object], ...],
    provenance: tuple[TranscriptProvenance, ...],
    replay_context: str,
) -> str:
    payload = {
        "messages": list(messages),
        "provenance": [item.to_dict() for item in provenance],
        "replay_context": replay_context,
    }
    return _stable_hash(payload)


def _canonicalize_evidence(value: object) -> object:
    if isinstance(value, Mapping):
        return {
            str(key): _canonicalize_evidence(nested)
            for key, nested in sorted(value.items(), key=lambda item: str(item[0]))
            if str(key) not in _FORBIDDEN_FOREIGN_KEYS
        }
    if isinstance(value, list):
        normalized = [_canonicalize_evidence(item) for item in value]
        if all(isinstance(item, (str, int, float, bool, type(None))) for item in normalized):
            return sorted(normalized, key=lambda item: json.dumps(item, ensure_ascii=True, sort_keys=True))
        return normalized
    if isinstance(value, tuple):
        return _canonicalize_evidence(list(value))
    return value


def _stable_id(prefix: str, provider: str, payload: object) -> str:
    return f"{prefix}:{provider}:{_stable_hash(payload)}"


def _stable_hash(payload: object) -> str:
    data = (json.dumps(_canonicalize_full(payload), ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()


def _safe_hash(payload: object) -> str:
    data = (json.dumps(_canonicalize_evidence(payload), ensure_ascii=True, sort_keys=True, separators=(",", ":")) + "\n").encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()


def _canonicalize_full(value: object) -> object:
    if isinstance(value, Mapping):
        return {str(key): _canonicalize_full(nested) for key, nested in sorted(value.items(), key=lambda item: str(item[0]))}
    if isinstance(value, list):
        return [_canonicalize_full(item) for item in value]
    if isinstance(value, tuple):
        return [_canonicalize_full(item) for item in value]
    return value
