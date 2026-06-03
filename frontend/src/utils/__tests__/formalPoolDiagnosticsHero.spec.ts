import { describe, expect, it } from 'vitest'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'
import {
  deriveFormalPoolDiagnosticsHero,
  type FormalPoolDiagnosticsActionKey,
} from '../formalPoolDiagnosticsHero'

function account(overrides: Partial<Account> = {}): Account {
  return {
    id: 42,
    name: 'formal-account',
    platform: 'anthropic',
    type: 'oauth',
    credentials: {},
    proxy_id: 7,
    concurrency: 1,
    priority: 0,
    status: 'error',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-06-01T00:00:00Z',
    updated_at: '2026-06-01T00:00:00Z',
    schedulable: false,
    effective_schedulable: false,
    is_formal_pool: true,
    onboarding_stage: 'quarantined',
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

function diagnostics(overrides: Partial<FormalPoolOperationsDiagnostics> = {}): FormalPoolOperationsDiagnostics {
  return {
    account_id: 42,
    is_formal_pool: true,
    onboarding_stage: 'quarantined',
    schedulable: false,
    effective_schedulable: false,
    failure_origin: 'upstream',
    checks: [],
    recommended_actions: [],
    ...overrides,
  }
}

const keys = (actions: ReadonlyArray<{ key: FormalPoolDiagnosticsActionKey }>) => actions.map((action) => action.key)

function expectForbidden(hero: ReturnType<typeof deriveFormalPoolDiagnosticsHero>, forbidden: FormalPoolDiagnosticsActionKey[]) {
  for (const action of forbidden) {
    expect(keys(hero.forbiddenActions)).toContain(action)
    expect(hero.primaryAction?.key).not.toBe(action)
    expect(keys(hero.secondaryActions)).not.toContain(action)
  }
}

describe('deriveFormalPoolDiagnosticsHero', () => {
  it('OAuth invalid_grant uses guide-only primary, optional recommended swap/runtime, and forbids one-click reauth', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ type: 'oauth' }),
      diagnostics: diagnostics({
        failure_origin: 'token_exchange',
        failure_code: 'invalid_grant',
        status_code_bucket: 'status_401',
        recommended_actions: [
          { key: 'reauthorize_oauth', label: 'Reauthorize OAuth', severity: 'danger' },
          { key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' },
          { key: 'runtime_register', label: 'Runtime register', severity: 'info' },
        ],
      }),
    })

    expect(hero.scenario).toBe('oauth_invalid_grant')
    expect(hero.primaryAction?.key).toBe('guideOAuthReauth')
    expect(hero.primaryAction?.behavior).toBe('guide')
    expect(keys(hero.secondaryActions)).toEqual(['swapProxy', 'runtimeRegister'])
    expectForbidden(hero, ['oneClickOAuthReauth'])
  })

  it('Setup Token expired uses replaceSetupToken primary, allows swapProxy, and forbids generic token replace', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ type: 'setup-token' }),
      diagnostics: diagnostics({
        failure_origin: 'token_exchange',
        failure_code: 'setup_token_expired',
        recommended_actions: [
          { key: 'replace_setup_token', label: 'Replace setup token', severity: 'danger' },
          { key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' },
        ],
      }),
    })

    expect(hero.scenario).toBe('setup_token_expired')
    expect(hero.primaryAction?.key).toBe('replaceSetupToken')
    expect(keys(hero.secondaryActions)).toContain('swapProxy')
    expectForbidden(hero, ['genericTokenReplace'])
  })

  it('5h rate-limited uses wait/no primary repair, allows refresh diagnostics, and forbids healthcheck', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account(),
      diagnostics: diagnostics({
        failure_origin: 'upstream',
        failure_code: 'long_context_usage_credits',
        status_code_bucket: 'status_429',
        formal_pool_rate_limit_window: '5h',
        recommended_actions: [{ key: 'wait_rate_limit', label: 'Wait', severity: 'warning' }],
      }),
    })

    expect(hero.scenario).toBe('rate_limited_5h')
    expect(hero.primaryAction?.key).toBe('wait')
    expect(hero.primaryAction?.behavior).toBe('none')
    expect(keys(hero.secondaryActions)).toEqual(['refreshDiagnostics'])
    expect(hero.rootCauseBullets.join('\n')).toContain('窗口：5 小时窗口（5h）')
    expect(hero.rootCauseBullets.join('\n')).not.toContain('未知状态（5h）')
    expectForbidden(hero, ['healthcheck'])
  })

  it('403 hold/KYC uses manual-only primary, allows quarantine, and forbids auto repair', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account(),
      diagnostics: diagnostics({
        failure_origin: 'upstream',
        failure_code: 'account_on_hold',
        status_code_bucket: 'status_403',
        quarantine_reason: 'kyc',
        risk_text_detected: true,
        recommended_actions: [{ key: 'quarantine', label: 'Quarantine', severity: 'danger' }],
      }),
    })

    expect(hero.scenario).toBe('manual_risk')
    expect(hero.primaryAction?.key).toBe('manualReview')
    expect(hero.primaryAction?.behavior).toBe('guide')
    expect(keys(hero.secondaryActions)).toEqual(['quarantine'])
    expectForbidden(hero, ['autoRepair', 'healthcheck'])
  })

  it('proxy mismatch/fallback repairs proxy first, then allows runtime-register/healthcheck sequence, forbidding direct healthcheck', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account(),
      diagnostics: diagnostics({
        failure_origin: 'proxy',
        fallback_detected: true,
        proxy_mismatch: true,
        recommended_actions: [
          { key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' },
          { key: 'runtime_register', label: 'Runtime register', severity: 'info' },
          { key: 'healthcheck', label: 'Healthcheck', severity: 'info' },
        ],
      }),
    })

    expect(hero.scenario).toBe('proxy_mismatch')
    expect(hero.primaryAction?.key).toBe('swapProxy')
    expect(keys(hero.secondaryActions)).toEqual(['runtimeRegisterThenHealthcheck'])
    expectForbidden(hero, ['directHealthcheckBeforeProxyRepair'])
  })

  it('evidence missing uses runtime-register before healthcheck when runtime evidence is incomplete and forbids promoteProduction', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ onboarding_stage: 'healthcheck_passed' }),
      diagnostics: diagnostics({
        onboarding_stage: 'healthcheck_passed',
        cc_gateway_seen: false,
        raw_capture_present: false,
        runtime_evidence_complete: false,
        recommended_actions: [
          { key: 'runtime_register', label: 'Runtime register', severity: 'info' },
          { key: 'healthcheck', label: 'Healthcheck', severity: 'info' },
          { key: 'promote_production', label: 'Promote', severity: 'info' },
        ],
      }),
    })

    expect(hero.scenario).toBe('evidence_missing')
    expect(hero.primaryAction?.key).toBe('runtimeRegister')
    expect(keys(hero.secondaryActions)).not.toContain('healthcheck')
    expectForbidden(hero, ['promoteProduction'])
  })

  it('evidence missing allows healthcheck only after runtime registration evidence is complete', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ onboarding_stage: 'healthcheck_passed' }),
      diagnostics: diagnostics({
        onboarding_stage: 'healthcheck_passed',
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T01:02:03Z',
        runtime_evidence_complete: true,
        raw_capture_present: false,
        healthcheck_evidence_persisted: false,
        recommended_actions: [{ key: 'healthcheck', label: 'Healthcheck', severity: 'info' }],
      }),
    })

    expect(hero.scenario).toBe('evidence_missing')
    expect(hero.primaryAction?.key).toBe('healthcheck')
    expectForbidden(hero, ['promoteProduction'])
  })

  it('evidence missing uses runtime-register primary when gateway runtime is unregistered without backend recommendations', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ onboarding_stage: 'healthcheck_passed' }),
      diagnostics: diagnostics({
        onboarding_stage: 'healthcheck_passed',
        cc_gateway_runtime_registered: false,
        healthcheck_evidence_persisted: false,
        recommended_actions: [],
      }),
    })

    expect(hero.scenario).toBe('evidence_missing')
    expect(hero.primaryAction?.key).toBe('runtimeRegister')
    expect(keys(hero.secondaryActions)).not.toContain('promoteProduction')
    expectForbidden(hero, ['promoteProduction'])
  })

  it('evidence missing uses runtime-register primary when gateway runtime timestamp is missing without backend recommendations', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ onboarding_stage: 'healthcheck_passed' }),
      diagnostics: diagnostics({
        onboarding_stage: 'healthcheck_passed',
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: false,
        recommended_actions: [],
      }),
    })

    expect(hero.scenario).toBe('evidence_missing')
    expect(hero.primaryAction?.key).toBe('runtimeRegister')
    expect(keys(hero.secondaryActions)).not.toContain('promoteProduction')
    expectForbidden(hero, ['promoteProduction'])
  })



  it('localizes proxy_mismatch and bucket_mismatch in hero bullets without making codes the primary copy', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account(),
      diagnostics: diagnostics({
        failure_origin: 'proxy_mismatch' as FormalPoolOperationsDiagnostics['failure_origin'],
        failure_code: 'bucket_mismatch',
        status_code_bucket: 'rate_limit_5h',
        proxy_mismatch: true,
        fallback_detected: true,
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' }],
      }),
    })

    const bullets = hero.rootCauseBullets.join('\n')
    expect(bullets).toContain('代理出口不一致')
    expect(bullets).toContain('出口分组不一致')
    expect(bullets).toContain('发现 fallback')
    expect(bullets).not.toContain('失败来源：proxy_mismatch')
    expect(bullets).not.toContain('失败分类：bucket_mismatch')
    expect(bullets).not.toContain('proxy_mismatch：true')
    expect(bullets).not.toContain('fallback_detected：true')
  })

  it('uses Chinese fallback copy for unknown diagnostic codes while retaining the code for troubleshooting', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account(),
      diagnostics: diagnostics({
        failure_origin: 'custom_origin' as FormalPoolOperationsDiagnostics['failure_origin'],
        failure_code: 'custom_bucket_mystery',
      }),
    })

    const bullets = hero.rootCauseBullets.join('\n')
    expect(bullets).toContain('未知来源（custom_origin）')
    expect(bullets).toContain('未知分类（custom_bucket_mystery）')
    expect(bullets).not.toContain('失败来源：custom_origin')
    expect(bullets).not.toContain('失败分类：custom_bucket_mystery')
  })

  it('monitor uses no primary repair, allows refresh diagnostics, and forbids all repair buttons', () => {
    const hero = deriveFormalPoolDiagnosticsHero({
      account: account({ status: 'active', schedulable: true, effective_schedulable: true, onboarding_stage: 'production' }),
      diagnostics: diagnostics({
        onboarding_stage: 'production',
        schedulable: true,
        effective_schedulable: true,
        recommended_actions: [{ key: 'monitor', label: 'Monitor', severity: 'info' }],
      }),
    })

    expect(hero.scenario).toBe('monitor')
    expect(hero.primaryAction?.key).toBe('none')
    expect(hero.primaryAction?.behavior).toBe('none')
    expect(keys(hero.secondaryActions)).toEqual(['refreshDiagnostics'])
    expectForbidden(hero, ['replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'promoteProduction', 'quarantine'])
  })
})
