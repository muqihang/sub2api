from __future__ import annotations

from dataclasses import dataclass
from typing import Mapping


class ProviderProtocolProbeError(RuntimeError):
    pass


_REQUIRED_DEEPSEEK_ANTHROPIC_CAPABILITIES = ("tools", "sse", "reasoning", "cache", "error_passthrough")


@dataclass(frozen=True, slots=True)
class ProviderProtocolProbe:
    protocol: str
    base_url: str
    capabilities_verified: bool
    live_runtime_verified: bool
    capabilities: Mapping[str, bool]


@dataclass(frozen=True, slots=True)
class ProviderProbeEntry:
    provider: str
    models: tuple[str, ...]
    preferred_protocol: str
    fallback_protocol: str
    protocols: Mapping[str, ProviderProtocolProbe]


@dataclass(frozen=True, slots=True)
class ProviderProbeCatalog:
    providers: Mapping[str, ProviderProbeEntry]


@dataclass(frozen=True, slots=True)
class BridgeTransportDecision:
    provider: str
    model_id: str
    selected_protocol: str
    base_url: str
    fallback_protocol: str
    fallback_reason: str
    capabilities: Mapping[str, bool]


def build_cp6_provider_probe_catalog(payload: Mapping[str, object]) -> ProviderProbeCatalog:
    if payload.get("mode") != "fixture_only_no_live_network":
        raise ProviderProtocolProbeError("CP6 provider probes must be fixture-only before CP8 live matrix")
    providers_raw = payload.get("providers")
    if not isinstance(providers_raw, Mapping):
        raise ProviderProtocolProbeError("provider probe catalog requires providers")
    providers: dict[str, ProviderProbeEntry] = {}
    for provider, raw_entry in providers_raw.items():
        if not isinstance(raw_entry, Mapping):
            raise ProviderProtocolProbeError("provider probe entry must be an object")
        models = raw_entry.get("models")
        if not isinstance(models, list) or not all(isinstance(model, str) and model for model in models):
            raise ProviderProtocolProbeError("provider probe entry requires models")
        preferred = _required_str(raw_entry, "preferred_protocol")
        fallback = _required_str(raw_entry, "fallback_protocol")
        protocols: dict[str, ProviderProtocolProbe] = {}
        for protocol in (preferred, fallback):
            raw_protocol = raw_entry.get(protocol)
            if not isinstance(raw_protocol, Mapping):
                raise ProviderProtocolProbeError("provider probe protocol entry is missing")
            probe = _build_protocol_probe(protocol, raw_protocol)
            if probe.live_runtime_verified:
                raise ProviderProtocolProbeError("live provider probes are not allowed in CP6")
            protocols[protocol] = probe
        providers[str(provider)] = ProviderProbeEntry(
            provider=str(provider),
            models=tuple(models),
            preferred_protocol=preferred,
            fallback_protocol=fallback,
            protocols=protocols,
        )
    return ProviderProbeCatalog(providers=providers)


def select_cp6_bridge_transport(catalog: ProviderProbeCatalog, *, provider: str, model_id: str) -> BridgeTransportDecision:
    entry = catalog.providers.get(provider)
    if entry is None:
        raise ProviderProtocolProbeError("unknown provider")
    if model_id not in entry.models:
        raise ProviderProtocolProbeError("unknown provider model")
    preferred = entry.protocols[entry.preferred_protocol]
    fallback = entry.protocols[entry.fallback_protocol]
    if not preferred.capabilities_verified:
        raise ProviderProtocolProbeError("provider capabilities are not verified")
    if provider == "deepseek" and entry.preferred_protocol == "anthropic_messages":
        for capability in _REQUIRED_DEEPSEEK_ANTHROPIC_CAPABILITIES:
            if not preferred.capabilities.get(capability, False):
                if not fallback.capabilities_verified:
                    raise ProviderProtocolProbeError("provider fallback capabilities are not verified")
                return BridgeTransportDecision(
                    provider=provider,
                    model_id=model_id,
                    selected_protocol=entry.fallback_protocol,
                    base_url=fallback.base_url,
                    fallback_protocol=entry.fallback_protocol,
                    fallback_reason=f"anthropic_{capability}_fixture_failed",
                    capabilities=fallback.capabilities,
                )
    return BridgeTransportDecision(
        provider=provider,
        model_id=model_id,
        selected_protocol=entry.preferred_protocol,
        base_url=preferred.base_url,
        fallback_protocol=entry.fallback_protocol,
        fallback_reason="",
        capabilities=preferred.capabilities,
    )


def _build_protocol_probe(protocol: str, raw: Mapping[str, object]) -> ProviderProtocolProbe:
    capabilities = raw.get("capabilities")
    if not isinstance(capabilities, Mapping):
        raise ProviderProtocolProbeError("provider probe capabilities are missing")
    return ProviderProtocolProbe(
        protocol=protocol,
        base_url=_required_str(raw, "base_url"),
        capabilities_verified=bool(raw.get("capabilities_verified")),
        live_runtime_verified=bool(raw.get("live_runtime_verified")),
        capabilities={str(key): bool(value) for key, value in capabilities.items()},
    )


def _required_str(raw: Mapping[str, object], key: str) -> str:
    value = raw.get(key)
    if not isinstance(value, str) or not value.strip():
        raise ProviderProtocolProbeError(f"provider probe {key} is required")
    return value.strip()
