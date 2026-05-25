#!/usr/bin/env python3
"""Productized runtime artifact generation for verified CLI-through modes."""
from __future__ import annotations

from pathlib import Path
from typing import Any, Mapping
import argparse
import json
import re
import stat


ACCOUNT_REF = 'opaque:account-ref:v1:placeholder'
BETA_PROFILE = 'claude_code_2_1_150_subscription_1m'
VERSION = '2.1.150'
VERSION_FAMILY = '2.1'
TRUSTED_MINOR_DRIFT_EXAMPLE = '2.1.151'
GATEWAY_TOKEN_PLACEHOLDER = '${CC_GATEWAY_TOKEN}'
PROXY_URL_PLACEHOLDER = '${CC_EGRESS_PROXY_URL}'
ALLOWED_CLAUDE_CODE_MODELS = [
    'claude-sonnet-4-6',
    'claude-opus-4-7',
    'claude-opus-4-7-thinking',
    'claude-opus-4-6',
    'claude-opus-4-6-thinking',
]
CANDIDATE_CLAUDE_CODE_MODELS = [
    'claude-sonnet-4-8',
    'claude-opus-4-8',
]
LOCALHOST_GUARDRAIL_ROLE = 'localhost_canary_guardrail_not_production_capability_ceiling'
_PLAIN_HASH_RE = re.compile(r'^(?:sha256|md5):', re.IGNORECASE)


class RuntimeConfigError(ValueError):
    pass


def _persona_resolver_contract() -> dict[str, Any]:
    return {
        'source_of_truth': 'cc_gateway_dynamic_persona_resolver',
        'registry_profile': BETA_PROFILE,
        'exact_version': VERSION,
        'version_family': VERSION_FAMILY,
        'trusted_minor_drift': {
            'example_version': TRUSTED_MINOR_DRIFT_EXAMPLE,
            'decision': 'observed_minor_drift',
            'capability_downgrade_allowed': False,
        },
        'unknown_major': {
            'decision': 'quarantine_version',
            'action': 'fail_closed',
        },
        'future_candidate_models': {
            'allowlist_ref': 'candidate_model_allowlist',
            'decision': 'gray_path',
            'capability_downgrade_allowed': False,
        },
    }


def build_runtime_manifest(*, mode: str, run_dir: Path, raw_dir: Path | None) -> dict[str, Any]:
    if mode not in {'localhost-preflight', 'real-canary', 'production-session'}:
        raise RuntimeConfigError(f'unsupported runtime mode: {mode}')
    requires_real = mode in {'real-canary', 'production-session'}
    if mode == 'real-canary' and raw_dir is None:
        raise RuntimeConfigError('real-canary mode requires an explicit raw_dir')
    upstream_mode = {
        'localhost-preflight': 'preflight',
        'real-canary': 'real-canary',
        'production-session': 'production',
    }[mode]
    upstream_url = 'http://127.0.0.1:19082' if mode == 'localhost-preflight' else 'https://api.anthropic.com'
    envelope = {
        'enabled': True,
        'max_tokens': 32000,
        'max_body_bytes': 131072,
        'max_tools_count': 40,
        'allow_thinking': True,
        'max_thinking_budget_tokens': 32000,
        'allow_output_config': True,
        'allow_context_management': True,
        'allow_context_1m': True,
        'max_context_window_tokens': 1_000_000,
        'allowed_models': ALLOWED_CLAUDE_CODE_MODELS,
    }
    manifest = {
        'schema_version': 1,
        'mode': mode,
        'run_dir': str(run_dir),
        'raw_dir': str(raw_dir) if raw_dir else '',
        'requires_real_anthropic': requires_real,
        'required_env': _required_env_manifest(mode, requires_real),
        'cc_gateway': {
            'mode': 'sub2api',
            'server': {'port': 18443, 'tls': {'cert': '', 'key': ''}},
            'upstream': {'url': upstream_url},
            'providers': {'anthropic': True},
            'auth': {'gateway_token': GATEWAY_TOKEN_PLACEHOLDER, 'tokens': []},
            'identity': {'device_id': '<from-account-identity>', 'email': 'redacted-email'},
            'env': {
                'platform': 'darwin', 'platform_raw': 'darwin', 'arch': 'arm64',
                'node_version': 'v22.22.2', 'terminal': 'iTerm2.app',
                'package_managers': 'npm', 'runtimes': 'node',
                'is_running_with_bun': False, 'is_ci': False, 'is_claude_ai_auth': True,
                'version': VERSION, 'version_base': VERSION,
                'deployment_environment': mode,
            },
            'prompt_env': {'platform': 'darwin', 'shell': 'zsh', 'working_dir': '/Users/redacted/projects'},
            'shared_pool': {
                'billing_cch_mode': 'sign',
                'signing_enabled': True,
                'signing_evidence_gates_approved': True,
                'upstream_mode': upstream_mode,
                'message_beta_profile': BETA_PROFILE,
                'persona_resolver': _persona_resolver_contract(),
                'candidate_model_allowlist': CANDIDATE_CLAUDE_CODE_MODELS,
                'candidate_model_replay_proofs': {
                    'claude-sonnet-4-8': 'fixture-sonnet-48',
                    'claude-opus-4-8': 'fixture-opus-48',
                },
                'candidate_model_kill_switches': {
                    'claude-sonnet-4-8': False,
                    'claude-opus-4-8': False,
                },
                'candidate_model_audit_budgets': {
                    'claude-sonnet-4-8': 25,
                    'claude-opus-4-8': 25,
                },
            },
            'account_identities': {
                ACCOUNT_REF: {
                    'device_id': '<from-account-identity>',
                    'account_uuid_ref': ACCOUNT_REF,
                    'account_ref': ACCOUNT_REF,
                    'account_ref_policy': 'server_scoped_hmac_with_key_id_scope_version_required',
                    'persona_variant': 'claude-code-2.1.150-macos-local',
                    'session_policy': 'preserve_downstream_session_id',
                    'policy_version': VERSION,
                }
            },
            'egress_buckets': {
                'home-ip-canary-2026-05-22': {
                    'enabled': True,
                    'proxy_url': PROXY_URL_PLACEHOLDER if requires_real else 'http://127.0.0.1:19083',
                    'allowed_account_ids': [ACCOUNT_REF],
                }
            },
            'logging': {'level': 'info', 'audit': True},
        },
        'session_budget': _session_budget_manifest(mode),
    }
    if mode == 'production-session':
        manifest['cc_gateway']['shared_pool']['production_upstream_enabled'] = True
        manifest['cc_gateway']['shared_pool']['production_budget'] = {
            'mode': 'observe_only',
            'enforcement_enabled': False,
            'p0_hard_block_only': True,
        }
    else:
        manifest['cc_gateway']['shared_pool']['max_body_bytes'] = 2097152
        manifest['cc_gateway']['shared_pool']['real_canary_user_approved'] = bool(mode == 'real-canary')
        manifest['cc_gateway']['shared_pool']['canary_envelope_role'] = LOCALHOST_GUARDRAIL_ROLE
        manifest['cc_gateway']['shared_pool']['canary_cost_envelope'] = envelope
    validate_runtime_manifest(manifest)
    return manifest


def _session_budget_manifest(mode: str) -> dict[str, Any]:
    if mode == 'production-session':
        return {
            'mode': 'observe_only',
            'enforcement_enabled': False,
            'hard_limits_source': 'operational_data_required',
            'normal_capability_policy': 'allow_tools_thinking_1m_opus_stream_max_tokens_32000',
            'p0_hard_block_only': True,
        }
    return {
        'mode': 'canary_limited',
        'enforcement_enabled': True,
        'max_messages_per_session': 2,
        'max_rich_messages_per_session': 1,
        'max_total_body_bytes_per_session': 256 * 1024,
        'hard_limits_source': 'local_canary_fixture_not_production',
    }


def _required_env_manifest(mode: str, requires_real: bool) -> dict[str, str]:
    env = {
        'CC_GATEWAY_TOKEN': '<required>',
        'CC_EGRESS_PROXY_URL': '<required>' if requires_real else '<optional-localhost-only>',
    }
    if mode == 'real-canary':
        env['ALLOW_REAL_ANTHROPIC_CANARY'] = '1'
    elif mode == 'production-session':
        env['ALLOW_REAL_ANTHROPIC_PRODUCTION'] = '1'
    else:
        env['ALLOW_REAL_ANTHROPIC_CANARY'] = '0'
        env['ALLOW_REAL_ANTHROPIC_PRODUCTION'] = '0'
    return env


def validate_runtime_manifest(manifest: Mapping[str, Any]) -> None:
    cc = manifest.get('cc_gateway')
    if not isinstance(cc, Mapping):
        raise RuntimeConfigError('missing cc_gateway config')
    shared = cc.get('shared_pool') if isinstance(cc.get('shared_pool'), Mapping) else {}
    env = cc.get('env') if isinstance(cc.get('env'), Mapping) else {}
    if shared.get('message_beta_profile') != BETA_PROFILE:
        raise RuntimeConfigError('message_beta_profile must use verified 2.1.150 subscription 1m-enabled profile')
    if env.get('version') != VERSION or env.get('version_base') != VERSION:
        raise RuntimeConfigError('runtime must pin verified 2.1.150 version fields until dynamic persona is implemented')
    if shared.get('billing_cch_mode') != 'sign' or shared.get('signing_enabled') is not True:
        raise RuntimeConfigError('CCH signing must be enabled')
    persona_resolver = shared.get('persona_resolver') if isinstance(shared.get('persona_resolver'), Mapping) else {}
    if persona_resolver.get('registry_profile') != BETA_PROFILE or persona_resolver.get('exact_version') != VERSION:
        raise RuntimeConfigError('runtime must pin persona_resolver to the verified 2.1.150 registry profile')
    if persona_resolver.get('version_family') != VERSION_FAMILY:
        raise RuntimeConfigError('runtime persona_resolver must declare the trusted 2.1 version family')
    trusted_minor = persona_resolver.get('trusted_minor_drift') if isinstance(persona_resolver.get('trusted_minor_drift'), Mapping) else {}
    if (
        trusted_minor.get('example_version') != TRUSTED_MINOR_DRIFT_EXAMPLE
        or trusted_minor.get('decision') != 'observed_minor_drift'
        or trusted_minor.get('capability_downgrade_allowed') is not False
    ):
        raise RuntimeConfigError('runtime must document trusted 2.1.151 minor drift as observed_minor_drift without capability downgrade')
    unknown_major = persona_resolver.get('unknown_major') if isinstance(persona_resolver.get('unknown_major'), Mapping) else {}
    if unknown_major.get('decision') != 'quarantine_version' or unknown_major.get('action') != 'fail_closed':
        raise RuntimeConfigError('runtime must fail closed on unknown major persona drift')
    future_candidates = persona_resolver.get('future_candidate_models') if isinstance(persona_resolver.get('future_candidate_models'), Mapping) else {}
    if (
        future_candidates.get('allowlist_ref') != 'candidate_model_allowlist'
        or future_candidates.get('decision') != 'gray_path'
        or future_candidates.get('capability_downgrade_allowed') is not False
    ):
        raise RuntimeConfigError('runtime must route future trusted Sonnet/Opus models through a gray path without capability downgrade')
    envelope = shared.get('canary_cost_envelope') if isinstance(shared.get('canary_cost_envelope'), Mapping) else {}
    if manifest.get('mode') != 'production-session':
        if (
            envelope.get('allow_context_1m') is not True
            or envelope.get('max_context_window_tokens') != 1_000_000
            or envelope.get('allow_thinking') is not True
            or envelope.get('allow_context_management') is not True
            or envelope.get('max_tokens') != 32000
        ):
            raise RuntimeConfigError('runtime must preserve the full Claude Code 1m/tools/thinking/context_management capability floor')
        allowed_models = envelope.get('allowed_models')
        if not isinstance(allowed_models, list) or not {'claude-opus-4-7', 'claude-opus-4-6'}.issubset(set(allowed_models)):
            raise RuntimeConfigError('runtime must list known Opus 4.7/4.6 model families in the canary envelope')
        if shared.get('canary_envelope_role') != LOCALHOST_GUARDRAIL_ROLE:
            raise RuntimeConfigError('runtime must declare canary envelope as a local guardrail, not a production capability ceiling')
    if manifest.get('mode') == 'real-canary':
        if shared.get('upstream_mode') != 'real-canary':
            raise RuntimeConfigError('real canary runtime must use real-canary upstream_mode')
        if shared.get('real_canary_user_approved') is not True:
            raise RuntimeConfigError('real canary runtime requires explicit user approval flag')
        if not manifest.get('raw_dir'):
            raise RuntimeConfigError('real canary runtime requires raw_dir')
    elif manifest.get('mode') == 'production-session':
        if shared.get('upstream_mode') != 'production':
            raise RuntimeConfigError('production runtime must use production upstream_mode')
        if shared.get('production_upstream_enabled') is not True:
            raise RuntimeConfigError('production runtime requires explicit production upstream enable flag')
        if 'real_canary_user_approved' in shared or shared.get('canary_cost_envelope') is not None:
            raise RuntimeConfigError('production runtime must not inherit real-canary approval or canary cost envelope')
        if shared.get('max_body_bytes') is not None:
            raise RuntimeConfigError('production runtime must not configure shared_pool.max_body_bytes as a hard body cap')
        production_budget = shared.get('production_budget') if isinstance(shared.get('production_budget'), Mapping) else {}
        if production_budget.get('mode') != 'observe_only' or production_budget.get('enforcement_enabled') is not False:
            raise RuntimeConfigError('production runtime must use observe-only production_budget by default')
    else:
        if shared.get('upstream_mode') != 'preflight':
            raise RuntimeConfigError('localhost runtime must use preflight upstream_mode')
        upstream = cc.get('upstream') if isinstance(cc.get('upstream'), Mapping) else {}
        if not str(upstream.get('url', '')).startswith('http://127.0.0.1:'):
            raise RuntimeConfigError('localhost runtime upstream must be loopback')
    session_budget = manifest.get('session_budget') if isinstance(manifest.get('session_budget'), Mapping) else {}
    if manifest.get('mode') == 'production-session':
        if session_budget.get('mode') != 'observe_only' or session_budget.get('enforcement_enabled') is not False:
            raise RuntimeConfigError('production session budget must default to observe_only with enforcement disabled')
        forbidden_limit_keys = {'max_messages_per_session', 'max_rich_messages_per_session', 'max_thinking_messages_per_session'}
        if forbidden_limit_keys & set(session_budget.keys()):
            raise RuntimeConfigError('production session budget must not contain low hard message/rich/thinking limits')
    candidate_models = shared.get('candidate_model_allowlist')
    if not isinstance(candidate_models, list) or not all(isinstance(item, str) for item in candidate_models):
        raise RuntimeConfigError('runtime must declare candidate_model_allowlist')
    replay_proofs = shared.get('candidate_model_replay_proofs')
    if not isinstance(replay_proofs, Mapping):
        raise RuntimeConfigError('runtime must declare candidate_model_replay_proofs')
    kill_switches = shared.get('candidate_model_kill_switches')
    if not isinstance(kill_switches, Mapping):
        raise RuntimeConfigError('runtime must declare candidate_model_kill_switches')
    audit_budgets = shared.get('candidate_model_audit_budgets')
    if not isinstance(audit_budgets, Mapping):
        raise RuntimeConfigError('runtime must declare candidate_model_audit_budgets')
    for model in candidate_models:
        if not str(replay_proofs.get(model, '')).strip():
            raise RuntimeConfigError(f'runtime must declare replay proof for candidate model {model}')
        if not isinstance(kill_switches.get(model), bool):
            raise RuntimeConfigError(f'runtime must declare bool kill switch for candidate model {model}')
        if not isinstance(audit_budgets.get(model), int) or audit_budgets.get(model) <= 0:
            raise RuntimeConfigError(f'runtime must declare positive audit budget for candidate model {model}')
    _reject_plain_hash_refs(manifest)


def render_cc_gateway_config(manifest: Mapping[str, Any]) -> str:
    validate_runtime_manifest(manifest)
    return _to_yaml(manifest['cc_gateway'])


def write_runtime_artifacts(manifest: Mapping[str, Any], output_dir: Path) -> dict[str, Path]:
    validate_runtime_manifest(manifest)
    output_dir.mkdir(parents=True, exist_ok=True)
    manifest_path = output_dir / 'runtime-manifest.json'
    config_path = output_dir / 'cc-gateway.yaml'
    start_path = output_dir / 'start-runtime.sh'
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True), encoding='utf-8')
    config_path.write_text(render_cc_gateway_config(manifest), encoding='utf-8')
    start_path.write_text(_render_start_script(manifest), encoding='utf-8')
    start_path.chmod(start_path.stat().st_mode | stat.S_IXUSR)
    return {'manifest': manifest_path, 'cc_config': config_path, 'start_script': start_path}


def _render_start_script(manifest: Mapping[str, Any]) -> str:
    mode = manifest['mode']
    real = bool(manifest['requires_real_anthropic'])
    raw_dir = manifest.get('raw_dir') or '${RUN_DIR}/raw'
    capture_snippet = ""
    if mode != 'production-session':
        capture_snippet = f'''
export CC_GATEWAY_RAW_CAPTURE_DIR={raw_dir!r}
mkdir -p "$CC_GATEWAY_RAW_CAPTURE_DIR"
chmod 700 "$CC_GATEWAY_RAW_CAPTURE_DIR"
'''
    if mode == 'real-canary':
        mode_guard = '''
if [[ "${ALLOW_REAL_ANTHROPIC_CANARY:-}" != "1" ]]; then
  echo "ALLOW_REAL_ANTHROPIC_CANARY=1 is required for $MODE" >&2
  exit 7
fi
'''
    elif mode == 'production-session':
        mode_guard = '''
if [[ "${ALLOW_REAL_ANTHROPIC_PRODUCTION:-}" != "1" ]]; then
  echo "ALLOW_REAL_ANTHROPIC_PRODUCTION=1 is required for $MODE" >&2
  exit 7
fi
'''
    else:
        mode_guard = '''
export ALLOW_REAL_ANTHROPIC_CANARY=0
export ALLOW_REAL_ANTHROPIC_PRODUCTION=0
'''
    return f'''#!/usr/bin/env bash
set -euo pipefail
MODE={mode!r}
: "${{CC_GATEWAY_TOKEN:?CC_GATEWAY_TOKEN is required}}"
if [[ "{str(real).lower()}" == "true" ]]; then
  : "${{CC_EGRESS_PROXY_URL:?CC_EGRESS_PROXY_URL is required for real modes}}"
fi
{mode_guard}
{capture_snippet}echo "runtime mode: $MODE"
echo "config: cc-gateway.yaml"
'''


def _reject_plain_hash_refs(value: Any, path: str = '$') -> None:
    if isinstance(value, Mapping):
        for key, item in value.items():
            if isinstance(key, str) and _PLAIN_HASH_RE.match(key.strip()):
                raise RuntimeConfigError(f'runtime must not include plain hash ref at {path}.{key}')
            _reject_plain_hash_refs(item, f'{path}.{key}')
        return
    if isinstance(value, list):
        for idx, item in enumerate(value):
            _reject_plain_hash_refs(item, f'{path}[{idx}]')
        return
    if isinstance(value, str) and _PLAIN_HASH_RE.match(value.strip()):
        raise RuntimeConfigError(f'runtime must not include plain hash ref at {path}')


def _to_yaml(value: Any, indent: int = 0) -> str:
    lines: list[str] = []
    prefix = ' ' * indent
    if isinstance(value, dict):
        for key, item in value.items():
            if isinstance(item, (dict, list)):
                lines.append(f'{prefix}{key}:')
                lines.append(_to_yaml(item, indent + 2).rstrip())
            else:
                lines.append(f'{prefix}{key}: {_yaml_scalar(item)}')
    elif isinstance(value, list):
        for item in value:
            if isinstance(item, (dict, list)):
                lines.append(f'{prefix}-')
                lines.append(_to_yaml(item, indent + 2).rstrip())
            else:
                lines.append(f'{prefix}- {_yaml_scalar(item)}')
    else:
        lines.append(f'{prefix}{_yaml_scalar(value)}')
    return '\n'.join(lines) + '\n'


def _yaml_scalar(value: Any) -> str:
    if value is True:
        return 'true'
    if value is False:
        return 'false'
    if value is None:
        return 'null'
    if isinstance(value, (int, float)):
        return str(value)
    text = str(value)
    if text == '' or any(ch in text for ch in [':', '#', '{', '}', '[', ']', ',', '&', '*', '!', '|', '>', "'", '"', '%', '@', '`', '$']):
        return json.dumps(text)
    return text


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description='Render verified CLI-through runtime artifacts')
    parser.add_argument('--mode', choices=('localhost-preflight', 'real-canary', 'production-session'), required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--raw-dir', type=Path)
    args = parser.parse_args(argv)
    manifest = build_runtime_manifest(mode=args.mode, run_dir=args.output_dir, raw_dir=args.raw_dir)
    paths = write_runtime_artifacts(manifest, args.output_dir)
    print(json.dumps({k: str(v) for k, v in paths.items()}, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
