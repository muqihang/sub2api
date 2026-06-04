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
CODEX_ROUTING_BRIDGE_INSTRUCTIONS = (
    "## Codex routing guidance\n\n"
    "- When Codex developer instructions include skills, plugins, MCP servers, or tool routing guidance, treat those sections as active routing guidance.\n"
    "- Before substantive work, quickly decide whether the user's request clearly matches any listed trigger. If it clearly matches a Skill, MUST read the matching SKILL.md before continuing; otherwise use the relevant plugin, MCP server, or tool first, then continue.\n"
    "- Do not load unrelated skills, do not repeatedly reload the same skill in the same turn, and do not use tools only for show.\n"
)
CODEX_CORE_BASE_INSTRUCTIONS = (
    "You are Codex, based on GPT-5. You are running as a coding agent in the Codex CLI on a user's computer.\n\n"
    "## General\n\n"
    "- When searching for text or files, prefer using `rg` or `rg --files` respectively because `rg` is much faster than alternatives like `grep`. "
    "(If the `rg` command is not found, then use alternatives.)\n\n"
    "- Act as an agent: inspect the workspace and use available tools to complete the user's task rather than only describing changes.\n"
    "- For multi-line file creation or rewrites, prefer a shell command such as `python3 - <<'PY' ... PY` when it is safer than many small edits; use `edit` or `apply_patch` for targeted changes.\n"
    "- For quick environment checks, use shell commands like `pwd`, `git status --short`, `ls -la`, and `rg --files` when relevant.\n\n"
    "## Editing constraints\n\n"
    "- Default to ASCII when editing or creating files. Only introduce non-ASCII or other Unicode characters when there is a clear justification and the file already uses them.\n"
    "- Add succinct code comments that explain what is going on if code is not self-explanatory. You should not add comments like \"Assigns the value to the variable\", but a brief comment might be useful ahead of a complex code block that the user would otherwise have to spend time parsing out. Usage of these comments should be rare.\n"
    "- Try to use `edit` for single file edits, but it is fine to explore other options if that does not fit well. Do not use `edit` for changes that are auto-generated (i.e. generating package.json or running a lint or format command like gofmt) or when scripting is more efficient.\n"
    "- You may be in a dirty git worktree. NEVER revert existing changes you did not make unless explicitly requested.\n"
    "- NEVER use destructive commands like `git reset --hard` or `git checkout --` unless specifically requested or approved by the user.\n\n"
    "## Presenting your work\n\n"
    "- Be concise and factual.\n"
    "- For substantial work, summarize what changed and why.\n"
    "- Offer next steps only when they are useful.\n"
)
CODEX_BASE_INSTRUCTIONS = CODEX_CORE_BASE_INSTRUCTIONS + "\n" + CODEX_ROUTING_BRIDGE_INSTRUCTIONS

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
        trusted_project_paths: list[str | Path] | None = None,
    ) -> ConfigPlan:
        provider = normalize_provider_id(str(profile.get("model_provider", CODEX_PROVIDER_ID) or CODEX_PROVIDER_ID))
        provider_name = str(profile.get("provider_display_name", CODEX_PROVIDER_NAME) or CODEX_PROVIDER_NAME)
        default_model = self._current_managed_model(provider) or str(profile.get("default_model", CODEX_DEFAULT_MODEL) or CODEX_DEFAULT_MODEL)
        reasoning_effort = self._current_managed_reasoning_effort(provider)
        catalog_path = self.catalog_path_for_profile(profile)
        catalog_payload = model_catalog_payload or {"models": []}
        default_limits = self._default_model_limits(catalog_payload, default_model)
        supports_websockets = CODEX_SUPPORTS_WEBSOCKETS
        feature_settings = self._feature_settings(enable_plugins=self._should_enable_plugins())
        feature_lines = ["[features]"]
        for key, value in feature_settings.items():
            rendered = toml_scalar(value)
            if rendered is not None:
                feature_lines.append(f"{key} = {rendered}")
        config_text = (
            f'model_provider = "{provider}"\n\n'
            f'model = "{default_model}"\n'
            f'model_context_window = {default_limits["context_window"]}\n'
            f'model_auto_compact_token_limit = {default_limits["auto_compact_token_limit"]}\n'
            f'model_catalog_json = "{catalog_path}"\n'
            + (f'model_reasoning_effort = "{reasoning_effort}"\n\n' if reasoning_effort else "\n")
            + "\n".join(feature_lines)
            + "\n\n"
            f'[model_providers.{provider}]\n'
            f'name = "{provider_name}"\n'
            f'base_url = "http://127.0.0.1:{local_proxy_port}/v1"\n'
            f'wire_api = "{profile.get("wire_api", "responses")}"\n'
            f'requires_openai_auth = {str(bool(profile.get("requires_openai_auth", True))).lower()}\n'
            f'supports_websockets = {str(supports_websockets).lower()}\n'
        )
        extra_sections = "\n".join(
            section
            for section in (
                self._project_sections(trusted_project_paths),
                self._marketplace_sections(),
                self._plugin_sections(),
            )
            if section
        )
        if extra_sections:
            config_text = f"{config_text}\n{extra_sections}"
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
        trusted_project_paths: list[str | Path] | None = None,
    ) -> None:
        if self.config_path.exists():
            try:
                tomllib.loads(self.config_path.read_text(encoding="utf-8"))
            except tomllib.TOMLDecodeError:
                pass
        plan = self.plan_configure(profile, local_proxy_port, loopback_secret, model_catalog_payload, trusted_project_paths)
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

    def _project_sections(self, trusted_project_paths: list[str | Path] | None = None) -> str:
        projects: dict[str, dict[str, object]] = {}
        if not self.config_path.exists():
            parsed_projects = None
        else:
            try:
                parsed = tomllib.loads(self.config_path.read_text(encoding="utf-8"))
                parsed_projects = parsed.get("projects")
            except (OSError, tomllib.TOMLDecodeError):
                parsed_projects = None
        if isinstance(parsed_projects, dict):
            for project_path, settings in parsed_projects.items():
                if isinstance(project_path, str) and isinstance(settings, dict):
                    projects[project_path] = dict(settings)
        for raw_path in trusted_project_paths or []:
            project_path = str(raw_path).strip()
            if project_path:
                projects.setdefault(project_path, {"trust_level": "trusted"})
        sections: list[str] = []
        for project_path, settings in projects.items():
            if not isinstance(project_path, str) or not isinstance(settings, dict):
                continue
            lines = [f"[projects.{toml_quote(project_path)}]"]
            for key, value in settings.items():
                if not isinstance(key, str):
                    continue
                rendered = toml_scalar(value)
                if rendered is None:
                    continue
                lines.append(f"{key} = {rendered}")
            if len(lines) > 1:
                sections.append("\n".join(lines))
        return "\n\n".join(sections) + ("\n" if sections else "")

    def _feature_settings(self, *, enable_plugins: bool) -> dict[str, object]:
        features: dict[str, object] = {}
        parsed_features = self._parsed_config_section("features")
        if isinstance(parsed_features, dict):
            features.update(parsed_features)
        features["responses_websockets_v2"] = CODEX_SUPPORTS_WEBSOCKETS
        if enable_plugins:
            features["plugins"] = True
        return features

    def _should_enable_plugins(self) -> bool:
        return bool(self._parsed_config_section("plugins")) or bool(self._parsed_config_section("marketplaces")) or self._bundled_marketplace_path().exists()

    def _marketplace_sections(self) -> str:
        marketplaces: dict[str, dict[str, object]] = {}
        parsed_marketplaces = self._parsed_config_section("marketplaces")
        if isinstance(parsed_marketplaces, dict):
            for name, settings in parsed_marketplaces.items():
                if isinstance(name, str) and isinstance(settings, dict):
                    marketplaces[name] = dict(settings)
        bundled = self._bundled_marketplace_path()
        if bundled.exists():
            marketplaces.setdefault(
                "openai-bundled",
                {
                    "source_type": "local",
                    "source": str(bundled),
                },
            )
            marketplaces["openai-bundled"]["source_type"] = "local"
            marketplaces["openai-bundled"]["source"] = str(bundled)
        return render_nested_sections("marketplaces", marketplaces)

    def _plugin_sections(self) -> str:
        plugins: dict[str, dict[str, object]] = {}
        parsed_plugins = self._parsed_config_section("plugins")
        if isinstance(parsed_plugins, dict):
            for name, settings in parsed_plugins.items():
                if isinstance(name, str) and isinstance(settings, dict):
                    plugins[name] = dict(settings)
        if self._bundled_marketplace_path().exists():
            for name in (
                "computer-use@openai-bundled",
                "browser@openai-bundled",
                "chrome@openai-bundled",
            ):
                plugins.setdefault(name, {"enabled": True})
        return render_nested_sections("plugins", plugins)

    def _parsed_config_section(self, name: str) -> object:
        if not self.config_path.exists():
            return None
        try:
            parsed = tomllib.loads(self.config_path.read_text(encoding="utf-8"))
        except (OSError, tomllib.TOMLDecodeError):
            return None
        return parsed.get(name)

    def _bundled_marketplace_path(self) -> Path:
        return self.codex_home / ".tmp" / "bundled-marketplaces" / "openai-bundled"

    def _current_managed_model(self, provider: str) -> str | None:
        parsed = self._parsed_config()
        if not parsed or parsed.get("model_provider") != provider:
            return None
        model = str(parsed.get("model") or "").strip()
        return model or None

    def _current_managed_reasoning_effort(self, provider: str) -> str | None:
        parsed = self._parsed_config()
        if not parsed or parsed.get("model_provider") != provider:
            return None
        effort = str(parsed.get("model_reasoning_effort") or "").strip()
        return effort or None

    def _parsed_config(self) -> dict[str, object] | None:
        if not self.config_path.exists():
            return None
        try:
            parsed = tomllib.loads(self.config_path.read_text(encoding="utf-8"))
        except (OSError, tomllib.TOMLDecodeError):
            return None
        return parsed

    def _catalog_model(self, model: dict[str, object]) -> dict[str, object]:
        if "base_instructions" in model and "model_messages" in model:
            return dict(model)
        slug = str(model.get("slug") or model.get("id") or model.get("model") or "")
        if not slug.strip():
            slug = "unknown-model"
        base_instructions = codex_base_instructions_for_model(model, slug)
        display_name = str(model.get("display_name") or model.get("displayName") or slug)
        context_window = safe_int(model.get("context_window") or model.get("max_context_window"), 0)
        catalog_model = {
            "slug": slug,
            "display_name": display_name,
            "description": str(model.get("description") or f"{display_name} via Zhumeng Codex."),
            "default_reasoning_level": str(model.get("default_reasoning_level") or "medium"),
            "supported_reasoning_levels": normalize_reasoning_levels(model.get("supported_reasoning_levels")),
            "shell_type": normalize_shell_type(model.get("shell_type")),
            "visibility": normalize_visibility(model.get("visibility")),
            "supported_in_api": bool(model.get("supported_in_api", True)),
            "priority": safe_int(model.get("priority"), 0),
            "base_instructions": base_instructions,
            "model_messages": {
                "instructions_template": base_instructions,
                "instructions_variables": {},
            },
            "context_window": context_window,
            "auto_compact_token_limit": safe_int(model.get("auto_compact_token_limit"), int(context_window * 0.85) if context_window > 0 else 0),
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
        for key in ("origin", "provider_id", "capabilities", "pricing"):
            if key in model:
                catalog_model[key] = model[key]
        return catalog_model

    def _default_model_limits(self, catalog_payload: dict[str, object], default_model: str) -> dict[str, int]:
        context_window = 200000
        auto_compact_token_limit = 150000
        models = catalog_payload.get("models", [])
        if isinstance(models, list):
            for model in models:
                if not isinstance(model, dict):
                    continue
                slug = str(model.get("slug") or model.get("id") or model.get("model") or "")
                if slug != default_model:
                    continue
                context_window = safe_int(model.get("context_window") or model.get("max_context_window"), context_window)
                auto_compact_token_limit = safe_int(
                    model.get("auto_compact_token_limit"),
                    int(context_window * 0.85),
                )
                break
        if context_window <= 0:
            context_window = 200000
        if auto_compact_token_limit <= 0 or auto_compact_token_limit >= context_window:
            auto_compact_token_limit = int(context_window * 0.85)
        return {
            "context_window": context_window,
            "auto_compact_token_limit": auto_compact_token_limit,
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


def normalize_shell_type(raw: object) -> str:
    shell_type = str(raw or "").strip()
    if shell_type in {"local", "shell_command"}:
        return shell_type
    return "local"


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


def codex_model_needs_routing_bridge(model: dict[str, object], slug: str) -> bool:
    providers = {
        str(model.get(key) or "").strip().lower()
        for key in ("provider_id", "provider", "origin")
        if str(model.get(key) or "").strip()
    }
    normalized_slug = slug.strip().lower()
    if providers:
        return bool(providers & {"deepseek", "anthropic"})
    return normalized_slug.startswith(("deepseek-", "claude-"))


def codex_base_instructions_for_model(model: dict[str, object], slug: str) -> str:
    if codex_model_needs_routing_bridge(model, slug):
        return CODEX_BASE_INSTRUCTIONS
    return CODEX_CORE_BASE_INSTRUCTIONS


def normalize_provider_id(raw: str) -> str:
    if raw in {"", "zhumeng-managed"}:
        return CODEX_PROVIDER_ID
    return raw


def toml_quote(value: str) -> str:
    return json.dumps(value, ensure_ascii=True)


def toml_scalar(value: object) -> str | None:
    if isinstance(value, bool):
        return str(value).lower()
    if isinstance(value, int) and not isinstance(value, bool):
        return str(value)
    if isinstance(value, float):
        return str(value)
    if isinstance(value, str):
        return toml_quote(value)
    return None


def render_nested_sections(prefix: str, sections_by_name: dict[str, dict[str, object]]) -> str:
    sections: list[str] = []
    for name, settings in sections_by_name.items():
        if not isinstance(name, str) or not isinstance(settings, dict):
            continue
        lines = [f"[{prefix}.{toml_quote(name)}]"]
        for key, value in settings.items():
            if not isinstance(key, str):
                continue
            rendered = toml_scalar(value)
            if rendered is None:
                continue
            lines.append(f"{key} = {rendered}")
        if len(lines) > 1:
            sections.append("\n".join(lines))
    return "\n\n".join(sections) + ("\n" if sections else "")


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


def discover_git_project_path(start: str | Path | None = None) -> Path | None:
    raw = Path(start) if start is not None else Path.cwd()
    try:
        current = raw.expanduser().resolve()
    except OSError:
        return None
    if not current.exists():
        return None
    if current.is_file():
        current = current.parent
    for candidate in (current, *current.parents):
        if (candidate / ".git").exists():
            return candidate
    return None
