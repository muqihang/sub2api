import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'

const {
  getDiagnostics,
  replaceSetupToken,
  runtimeRegister,
  healthcheck,
  swapProxy,
  quarantine,
} = vi.hoisted(() => ({
  getDiagnostics: vi.fn(),
  replaceSetupToken: vi.fn(),
  runtimeRegister: vi.fn(),
  healthcheck: vi.fn(),
  swapProxy: vi.fn(),
  quarantine: vi.fn(),
}))

vi.mock('@/api/admin/formalPoolOperations', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/formalPoolOperations')>('@/api/admin/formalPoolOperations')
  return {
    ...actual,
    getDiagnostics,
    replaceSetupToken,
    runtimeRegister,
    healthcheck,
    swapProxy,
    quarantine,
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

import FormalPoolDiagnosticsModalV2 from '../FormalPoolDiagnosticsModalV2.vue'

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
    failure_origin: 'token_exchange',
    failure_code: 'invalid_grant',
    status_code_bucket: 'status_401',
    checks: [{ name: 'stage_gate', status: 'fail', message: 'invalid_grant' }],
    recommended_actions: [{ key: 'reauthorize_oauth', label: 'Reauthorize OAuth', severity: 'danger' }],
    ...overrides,
  }
}

async function mountModal(options: { account?: Account; diagnostics?: FormalPoolOperationsDiagnostics } = {}) {
  getDiagnostics.mockResolvedValue(options.diagnostics ?? diagnostics())
  const wrapper = mount(FormalPoolDiagnosticsModalV2, {
    props: { show: true, account: options.account ?? account() },
  })
  await flushPromises()
  return wrapper
}

describe('FormalPoolDiagnosticsModalV2', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders a compact command bar with environment badge, generated time, refresh state, and primary action', async () => {
    const wrapper = await mountModal({ diagnostics: diagnostics({ generated_at: '2026-06-01T14:32:08Z' } as Partial<FormalPoolOperationsDiagnostics>) })

    expect(wrapper.find('[data-testid="diagnostics-v2-command-bar"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="diagnostics-v2-environment"]').text()).toContain('正式号池')
    expect(wrapper.get('[data-testid="diagnostics-v2-generated-at"]').text()).toContain('2026-06-01T14:32:08Z')
    expect(wrapper.get('[data-testid="diagnostics-v2-refresh-state"]').text()).toContain('已刷新')
    expect(wrapper.get('[data-testid="diagnostics-v2-primary-action"]').text()).toContain('查看 OAuth 重新授权步骤')
  })

  it('separates root-cause analysis from allowed actions and does not render OAuth one-click reauth', async () => {
    const wrapper = await mountModal()

    expect(wrapper.find('[data-testid="diagnostics-v2-root-cause"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="diagnostics-v2-allowed-actions"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="diagnostics-v2-root-cause"]').text()).toContain('OAuth')
    expect(wrapper.text()).toContain('当前后端无一键 OAuth 重新授权 API')
    expect(wrapper.find('[data-testid="action-oneClickOAuthReauth"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('一键重新授权')
  })

  it('keeps evidence collapsed by default, then supports grouped evidence search', async () => {
    const wrapper = await mountModal({
      diagnostics: diagnostics({
        failure_origin: 'proxy',
        failure_code: 'proxy_mismatch',
        cc_gateway_seen: true,
        raw_capture_present: true,
        proxy_mismatch: true,
        fallback_detected: true,
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' }],
      }),
    })

    expect(wrapper.find('[data-testid="evidence-search"]').exists()).toBe(false)
    await wrapper.get('[data-testid="evidence-toggle"]').trigger('click')
    expect(wrapper.find('[data-testid="evidence-group-lifecycle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="evidence-group-gateway"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="evidence-group-proxy"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="evidence-group-upstream"]').exists()).toBe(true)

    await wrapper.get('[data-testid="evidence-search"]').setValue('proxy mismatch')
    expect(wrapper.get('[data-testid="evidence-group-proxy"]').text()).toContain('proxy_mismatch')
    expect(wrapper.find('[data-testid="evidence-item-cc_gateway_seen"]').exists()).toBe(false)
  })

  it('does not show forbidden healthcheck for 5h rate limits or direct healthcheck for proxy mismatch', async () => {
    const rateLimited = await mountModal({
      diagnostics: diagnostics({
        failure_origin: 'upstream',
        failure_code: 'long_context_usage_credits',
        status_code_bucket: 'status_429',
        formal_pool_rate_limit_window: '5h',
        recommended_actions: [{ key: 'wait_rate_limit', label: 'Wait', severity: 'warning' }],
      }),
    })
    expect(rateLimited.text()).toContain('等待 5h 用量窗口恢复')
    expect(rateLimited.find('[data-testid="action-healthcheck"]').exists()).toBe(false)
    rateLimited.unmount()

    const proxyMismatch = await mountModal({
      diagnostics: diagnostics({
        failure_origin: 'proxy',
        proxy_mismatch: true,
        fallback_detected: true,
        recommended_actions: [
          { key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' },
          { key: 'healthcheck', label: 'Healthcheck', severity: 'info' },
        ],
      }),
    })
    expect(proxyMismatch.find('[data-testid="action-swapProxy"]').exists()).toBe(true)
    expect(proxyMismatch.find('[data-testid="action-healthcheck"]').exists()).toBe(false)
    expect(proxyMismatch.text()).toContain('更换代理后再执行 runtime-register / healthcheck')
  })

  it('does not render promoteProduction in the DOM when evidence is missing', async () => {
    const wrapper = await mountModal({
      diagnostics: diagnostics({
        failure_origin: 'control_plane',
        failure_code: 'runtime_evidence_incomplete',
        status_code_bucket: 'status_2xx',
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: false,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        runtime_evidence_complete: false,
        recommended_actions: [{ key: 'promote_production', label: 'Promote production', severity: 'info' }],
      }),
    })

    expect(wrapper.get('[data-testid="diagnostics-v2-root-cause"]').text()).toContain('evidence_missing')
    expect(wrapper.find('[data-testid="action-promoteProduction"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('进入生产')
  })

  it('does not render any repair buttons in the DOM when diagnostics recommend monitor', async () => {
    const wrapper = await mountModal({
      account: account({ status: 'active', schedulable: true, effective_schedulable: true, onboarding_stage: 'production' }),
      diagnostics: diagnostics({
        onboarding_stage: 'production',
        schedulable: true,
        effective_schedulable: true,
        failure_origin: 'unknown',
        failure_code: undefined,
        status_code_bucket: undefined,
        recommended_actions: [{ key: 'monitor', label: 'Monitor', severity: 'info' }],
      }),
    })

    for (const action of ['replaceSetupToken', 'swapProxy', 'runtimeRegister', 'healthcheck', 'promoteProduction', 'quarantine']) {
      expect(wrapper.find(`[data-testid="action-${action}"]`).exists()).toBe(false)
    }
  })

  it('normalizes a quarantine action result that returns an Account into the V2 operation result flow', async () => {
    quarantine.mockResolvedValue({ account: account({ onboarding_stage: 'quarantined', quarantine_reason: 'kyc' }) })
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      failure_origin: 'upstream',
      failure_code: 'account_on_hold',
      status_code_bucket: 'status_403',
      quarantine_reason: 'kyc',
      risk_text_detected: true,
      recommended_actions: [{ key: 'quarantine', label: 'Quarantine', severity: 'danger' }],
    })).mockResolvedValueOnce(diagnostics({
      onboarding_stage: 'quarantined',
      quarantine_reason: 'kyc',
      recommended_actions: [{ key: 'monitor', label: 'Monitor', severity: 'info' }],
    }))
    const wrapper = mount(FormalPoolDiagnosticsModalV2, { props: { show: true, account: account() } })
    await flushPromises()

    await wrapper.get('[data-testid="action-quarantine"]').trigger('click')
    await flushPromises()

    expect(quarantine).toHaveBeenCalledWith(42, expect.stringContaining('manual-risk'))
    expect(getDiagnostics).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ id: 42, onboarding_stage: 'quarantined' })
  })

  it('scrubs sensitive backend and account text at DOM level', async () => {
    const rawFragments = [
      'sk-ant-secret-DONTLEAK',
      'sk-ant-sid-secret-DONTLEAK',
      'operator@example.com',
      '123e4567-e89b-12d3-a456-426614174000',
      'raw_prompt',
      'raw_body',
      'raw_telemetry',
      'raw_cch',
      'user:proxy-pass@',
      'secret-password',
    ]
    const wrapper = await mountModal({
      account: account({ name: 'operator@example.com sk-ant-secret-DONTLEAK' }),
      diagnostics: diagnostics({
        account_ref: '123e4567-e89b-12d3-a456-426614174000',
        failure_code: 'invalid_grant sk-ant-sid-secret-DONTLEAK',
        failure_source: 'raw_prompt raw_body',
        raw_capture_ref: 'raw_telemetry raw_cch',
        risk_event_ref: 'proxy http://user:proxy-pass@example.net:8080 password=secret-password',
        checks: [{ name: 'raw_prompt', status: 'fail', message: 'operator@example.com raw_body' }],
        recommended_actions: [{ key: 'reauthorize_oauth', label: 'Reauthorize OAuth', severity: 'danger' }],
      }),
    })
    await wrapper.get('[data-testid="evidence-toggle"]').trigger('click')

    const html = wrapper.html()
    for (const fragment of rawFragments) {
      expect(html).not.toContain(fragment)
    }
    expect(html).toContain('[redacted]')
  })
})
