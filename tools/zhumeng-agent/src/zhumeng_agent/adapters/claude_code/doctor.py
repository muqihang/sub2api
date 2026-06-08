from __future__ import annotations

from dataclasses import dataclass, field

from .profile import ClaudeCodeCapabilityProfile, ToolSearchMode


@dataclass(frozen=True, slots=True)
class ClaudeCodeDoctorContext:
    model: str
    claude_code_version: str
    has_mcp_deferred_tools: bool
    has_pending_mcp_server: bool
    disallowed_tools: set[str] = field(default_factory=set)
    model_supports_tool_reference: bool = True


@dataclass(frozen=True, slots=True)
class ToolSearchDecision:
    env_value: str
    status: str
    degraded: bool
    reasons: tuple[str, ...] = field(default_factory=tuple)


def evaluate_toolsearch_profile(
    capability: ClaudeCodeCapabilityProfile,
    context: ClaudeCodeDoctorContext,
) -> ToolSearchDecision:
    if capability.tool_search_mode == ToolSearchMode.STANDARD:
        return ToolSearchDecision(env_value="false", status="ready", degraded=False)
    if capability.tool_search_mode == ToolSearchMode.AUTO:
        return ToolSearchDecision(env_value="auto", status="ready", degraded=False)

    reasons: list[str] = []
    status = "toolsearch_degraded"
    if not _version_family_matches(capability.claude_code_version_family, context.claude_code_version):
        reasons.append("version_family_mismatch")
        status = "profile_mismatch"
    if not capability.tool_search_healthcheck_passed:
        reasons.append("healthcheck")
    if not (context.has_mcp_deferred_tools or context.has_pending_mcp_server):
        reasons.append("no_deferred_tools")
    if not context.model_supports_tool_reference or "haiku" in context.model.lower():
        reasons.append("model_unsupported")
    if "ToolSearchTool" in context.disallowed_tools or "toolsearch" in {item.lower() for item in context.disallowed_tools}:
        reasons.append("toolsearch_disallowed")
    if capability.kill_switches:
        reasons.append("kill_switch")

    if reasons:
        return ToolSearchDecision(
            env_value="auto",
            status=status,
            degraded=True,
            reasons=tuple(reasons),
        )
    return ToolSearchDecision(env_value="true", status="ready", degraded=False)


def _version_family_matches(version_family: str, version: str) -> bool:
    family = version_family.strip().lower()
    actual = version.strip().lower()
    if not family or family in {"*", "any"}:
        return True
    if family.endswith(".x"):
        return actual.startswith(family[:-1])
    return actual == family
