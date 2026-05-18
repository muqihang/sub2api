from __future__ import annotations

import json
import os
import socket
import tempfile
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

import tomllib

from .detect import resolve_codex_home

COMMON_PROXY_PORTS = {1080, 1086, 7890, 7891, 7897, 8080, 9090}
CODEX_PROVIDER_ID = "zhumeng-codex"
CODEX_PROVIDER_NAME = "Zhumeng Codex"
CODEX_DEFAULT_MODEL = "deepseek-v4-pro"
CODEX_MODEL_CATALOG_FILE = "zhumeng-codex-models.json"
CODEX_SUPPORTS_WEBSOCKETS = False
CODEX_BASE_INSTRUCTIONS = (
    "You are Codex, a coding agent. Work in the user's workspace, inspect the code before changing it, "
    "use available tools carefully, preserve unrelated user changes, and carry coding tasks through "
    "implementation and verification."
)

REASONING_DESCRIPTIONS = {
    "none": "Disable extended thinking",
    "minimal": "Minimal reasoning for fast simple tasks",
    "low": "Fast responses with lighter reasoning",
    "medium": "Balances speed and reasoning depth for everyday tasks",
    "high": "Greater reasoning depth for complex problems",
    "xhigh": "Extra high reasoning depth for complex problems",
}


@dataclass(slots=True)
class ConfigPlan:
    config_text: str
    auth_payload: dict[str, str]
    model_catalog_path: Path
    model_catalog_payload: dict[str, object]
    backup_paths: list[Path]


class CodexConfigManager:
    def __init__(self, codex_home: Path | None = None):
        self.codex_home = codex_home or resolve_codex_home()
        self.config_path = self.codex_home / "config.toml"
        self.auth_path = self.codex_home / "auth.json"
        self.backup_dir = self.codex_home / "backups"

    def catalog_path_for_profile(self, profile: dict[str, object] | None = None) -> Path:
        profile = profile or {}
        return self.codex_home / str(profile.get("model_catalog_file", CODEX_MODEL_CATALOG_FILE) or CODEX_MODEL_CATALOG_FILE)

    def read_state(self) -> dict[str, object]:
        return {
            "codex_home": str(self.codex_home),
            "config_exists": self.config_path.exists(),
            "auth_exists": self.auth_path.exists(),
        }

    def plan_configure(
        self,
        profile: dict[str, object],
        local_proxy_port: int,
        loopback_secret: str,
        model_catalog_payload: dict[str, object] | None = None,
    ) -> ConfigPlan:
        provider = normalize_provider_id(str(profile.get("model_provider", CODEX_PROVIDER_ID) or CODEX_PROVIDER_ID))
        provider_name = str(profile.get("provider_display_name", CODEX_PROVIDER_NAME) or CODEX_PROVIDER_NAME)
        default_model = str(profile.get("default_model", CODEX_DEFAULT_MODEL) or CODEX_DEFAULT_MODEL)
        catalog_path = self.catalog_path_for_profile(profile)
        catalog_payload = model_catalog_payload or {"models": []}
        supports_websockets = CODEX_SUPPORTS_WEBSOCKETS
        config_text = (
            f'model_provider = "{provider}"\n\n'
            f'model = "{default_model}"\n'
            f'model_catalog_json = "{catalog_path}"\n\n'
            '[features]\n'
            f'responses_websockets_v2 = {str(supports_websockets).lower()}\n\n'
            f'[model_providers.{provider}]\n'
            f'name = "{provider_name}"\n'
            f'base_url = "http://127.0.0.1:{local_proxy_port}/v1"\n'
            f'wire_api = "{profile.get("wire_api", "responses")}"\n'
            f'requires_openai_auth = {str(bool(profile.get("requires_openai_auth", True))).lower()}\n'
            f'supports_websockets = {str(supports_websockets).lower()}\n'
        )
        auth_payload = {
            "OPENAI_API_KEY": f"zhumeng-local-managed-{loopback_secret}",
        }
        return ConfigPlan(
            config_text=config_text,
            auth_payload=auth_payload,
            model_catalog_path=catalog_path,
            model_catalog_payload=catalog_payload,
            backup_paths=[],
        )

    def apply_configure(self, plan: ConfigPlan) -> None:
        self.codex_home.mkdir(parents=True, exist_ok=True)
        plan.backup_paths.extend(self._backup_existing())
        self._write_json_atomic(plan.model_catalog_path, plan.model_catalog_payload)
        self._write_text_atomic(self.config_path, plan.config_text)
        self._write_json_atomic(self.auth_path, plan.auth_payload)

    def write_model_catalog(self, profile: dict[str, object] | None, gateway_models_payload: dict[str, object]) -> dict[str, object]:
        catalog_payload = self.build_model_catalog(gateway_models_payload)
        self._write_json_atomic(self.catalog_path_for_profile(profile), catalog_payload)
        return catalog_payload

    def repair(
        self,
        profile: dict[str, object],
        local_proxy_port: int,
        loopback_secret: str,
        model_catalog_payload: dict[str, object] | None = None,
    ) -> None:
        if self.config_path.exists():
            try:
                tomllib.loads(self.config_path.read_text(encoding="utf-8"))
            except tomllib.TOMLDecodeError:
                pass
        plan = self.plan_configure(profile, local_proxy_port, loopback_secret, model_catalog_payload)
        self.apply_configure(plan)

    def build_model_catalog(self, gateway_models_payload: dict[str, object]) -> dict[str, object]:
        models = gateway_models_payload.get("models", [])
        if not isinstance(models, list):
            return {"models": []}
        return {"models": [self._catalog_model(model) for model in models if isinstance(model, dict)]}

    def read_existing_model_catalog(self, profile: dict[str, object] | None = None) -> dict[str, object]:
        catalog_path = self.catalog_path_for_profile(profile)
        if not catalog_path.exists():
            return {"models": []}
        try:
            payload = json.loads(catalog_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return {"models": []}
        if not isinstance(payload, dict):
            return {"models": []}
        return self.build_model_catalog(payload)

    def restore_backup(self, backup_path: Path) -> None:
        target = self.config_path if backup_path.name.startswith("config.toml") else self.auth_path
        self._write_text_atomic(target, backup_path.read_text(encoding="utf-8"))

    def _backup_existing(self) -> list[Path]:
        self.backup_dir.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now(UTC).strftime("%Y%m%d%H%M%S")
        backups: list[Path] = []
        for source in (self.config_path,):
            if not source.exists():
                continue
            backup = self.backup_dir / f"{source.name}.{timestamp}.bak"
            backup.write_text(source.read_text(encoding="utf-8"), encoding="utf-8")
            backups.append(backup)
        return backups

    def _write_text_atomic(self, path: Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        fd, temp_path = tempfile.mkstemp(prefix=path.name, suffix=".tmp", dir=str(path.parent))
        try:
            with os.fdopen(fd, "w", encoding="utf-8") as handle:
                handle.write(content)
            if os.name == "posix":
                os.chmod(temp_path, 0o600)
            os.replace(temp_path, path)
        finally:
            if os.path.exists(temp_path):
                os.unlink(temp_path)

    def _write_json_atomic(self, path: Path, payload: dict[str, str]) -> None:
        self._write_text_atomic(path, json.dumps(payload, ensure_ascii=True, indent=2))

    def _catalog_model(self, model: dict[str, object]) -> dict[str, object]:
        slug = str(model.get("slug") or model.get("id") or model.get("model") or "")
        if not slug.strip():
            slug = "unknown-model"
        display_name = str(model.get("display_name") or model.get("displayName") or slug)
        context_window = safe_int(model.get("context_window") or model.get("max_context_window"), 0)
        return {
            "slug": slug,
            "display_name": display_name,
            "description": str(model.get("description") or f"{display_name} via Zhumeng Codex."),
            "default_reasoning_level": str(model.get("default_reasoning_level") or "medium"),
            "supported_reasoning_levels": normalize_reasoning_levels(model.get("supported_reasoning_levels")),
            "shell_type": "shell_command",
            "visibility": normalize_visibility(model.get("visibility")),
            "supported_in_api": bool(model.get("supported_in_api", True)),
            "priority": safe_int(model.get("priority"), 0),
            "base_instructions": CODEX_BASE_INSTRUCTIONS,
            "model_messages": {
                "instructions_template": CODEX_BASE_INSTRUCTIONS,
                "instructions_variables": {},
            },
            "context_window": context_window,
            "max_context_window": safe_int(model.get("max_context_window"), context_window),
            "effective_context_window_percent": safe_int(model.get("effective_context_window_percent"), 95),
            "max_output_tokens": safe_int(model.get("max_output_tokens"), 128000),
            "support_verbosity": bool(model.get("support_verbosity", False)),
            "apply_patch_tool_type": str(model.get("apply_patch_tool_type") or "freeform"),
            "truncation_policy": model.get("truncation_policy") or {"mode": "tokens", "limit": 10000},
            "supports_parallel_tool_calls": bool(model.get("supports_parallel_tool_calls", False)),
            "supports_image_detail_original": bool(model.get("supports_image_detail_original", False)),
            "supports_reasoning_summaries": bool(model.get("supports_reasoning_summaries", True)),
            "default_reasoning_summary": str(model.get("default_reasoning_summary") or "none"),
            "experimental_supported_tools": model.get("experimental_supported_tools") or [],
            "input_modalities": model.get("input_modalities") or ["text"],
            "supports_search_tool": bool(model.get("supports_search_tool", False)),
            "web_search_tool_type": normalize_web_search_tool_type(model),
        }


def normalize_reasoning_levels(raw: object) -> list[dict[str, str]]:
    if not isinstance(raw, list) or not raw:
        raw = ["medium"]
    levels: list[dict[str, str]] = []
    for item in raw:
        if isinstance(item, dict):
            effort = str(item.get("effort") or item.get("reasoningEffort") or "")
            description = str(item.get("description") or REASONING_DESCRIPTIONS.get(effort, effort))
        else:
            effort = str(item)
            description = REASONING_DESCRIPTIONS.get(effort, effort)
        if effort:
            levels.append({"effort": effort, "description": description})
    return levels or [{"effort": "medium", "description": REASONING_DESCRIPTIONS["medium"]}]


def normalize_visibility(raw: object) -> str:
    if str(raw).lower() in {"hidden", "hide"}:
        return "hidden"
    return "list"


def normalize_web_search_tool_type(model: dict[str, object]) -> str | None:
    raw = str(model.get("web_search_tool_type") or "").strip()
    if raw in {"text", "text_and_image"}:
        return raw
    if not bool(model.get("supports_search_tool", False)):
        return None
    modalities = model.get("input_modalities")
    if isinstance(modalities, list) and "image" in {str(item) for item in modalities}:
        return "text_and_image"
    if bool(model.get("supports_image_detail_original", False)):
        return "text_and_image"
    return "text"


def normalize_provider_id(raw: str) -> str:
    if raw in {"", "zhumeng-managed"}:
        return CODEX_PROVIDER_ID
    return raw


def safe_int(value: object, default: int = 0) -> int:
    if value is None:
        return default
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, (int, float)):
        return int(value)
    text = str(value).strip()
    if not text:
        return default
    try:
        return int(text)
    except ValueError:
        try:
            return int(float(text))
        except ValueError:
            return default


def choose_local_proxy_port(preferred: int | None = None) -> int:
    if preferred and preferred not in COMMON_PROXY_PORTS and preferred > 0:
        return preferred

    while True:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        if port not in COMMON_PROXY_PORTS:
            return port
