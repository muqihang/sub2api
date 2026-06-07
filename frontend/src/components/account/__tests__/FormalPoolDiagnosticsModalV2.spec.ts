import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'

// Stub the project ConfirmDialog at module scope so every test in this file
// gets the same lightweight stand-in. The real ConfirmDialog uses BaseDialog +
// vue-i18n and would otherwise crash without a configured i18n plugin.
vi.mock('@/components/common/ConfirmDialog.vue', () => ({
  default: defineComponent({
    name: 'ConfirmDialog',
    props: ['show', 'title', 'message', 'confirmText', 'cancelText', 'danger', 'zIndex'],
    emits: ['confirm', 'cancel'],
    template:
      '<div v-if="show" data-testid="confirm-dialog-stub" :data-title="title" :data-message="message" :data-danger="String(danger)" :data-z-index="String(zIndex ?? \'\')" :style="{ zIndex }">' +
      '<button data-testid="confirm-dialog-stub-confirm" @click="$emit(\'confirm\')">{{ confirmText }}</button>' +
      '<button data-testid="confirm-dialog-stub-cancel" @click="$emit(\'cancel\')">{{ cancelText }}</button>' +
      '</div>',
  }),
}))

const {
  getDiagnostics,
  replaceSetupToken,
  runtimeRegister,
  healthcheck,
  startWarming,
  swapProxy,
  quarantine,
  promoteProduction,
  routerPush,
} = vi.hoisted(() => ({
  getDiagnostics: vi.fn(),
  replaceSetupToken: vi.fn(),
  runtimeRegister: vi.fn(),
  healthcheck: vi.fn(),
  startWarming: vi.fn(),
  swapProxy: vi.fn(),
  quarantine: vi.fn(),
  promoteProduction: vi.fn(),
  routerPush: vi.fn(),
}))

vi.mock('@/api/admin/formalPoolOperations', async () => {
  const actual = await vi.importActual<typeof import('@/api/admin/formalPoolOperations')>('@/api/admin/formalPoolOperations')
  return {
    ...actual,
    getDiagnostics,
    replaceSetupToken,
    runtimeRegister,
    healthcheck,
    startWarming,
    swapProxy,
    quarantine,
    promoteProduction,
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
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

// Diagnostics fixture that drives the hero into the evidence_missing scenario
// where `runtime_evidence_complete` is true but `healthcheck_evidence_persisted`
// is false. That picks `healthcheck` as the primary action so we can exercise
// the confirm flow without changing the hero's safety semantics.
function healthcheckScenarioDiagnostics(): FormalPoolOperationsDiagnostics {
  return diagnostics({
    onboarding_stage: 'runtime_registered',
    schedulable: false,
    effective_schedulable: false,
    failure_origin: 'control_plane',
    failure_code: 'healthcheck_evidence_missing',
    status_code_bucket: 'status_2xx',
    cc_gateway_seen: true,
    cc_gateway_runtime_registered: true,
    cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
    runtime_evidence_complete: true,
    healthcheck_evidence_persisted: false,
    raw_capture_present: true,
    recommended_actions: [{ key: 'healthcheck', label: 'Healthcheck', severity: 'info' }],
  })
}

describe('FormalPoolDiagnosticsModalV2', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    routerPush.mockResolvedValue(undefined)
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

  it('renders replace-account-and-proxy guidance as a navigation action', async () => {
    const wrapper = await mountModal({
      account: account({ type: 'setup-token' }),
      diagnostics: diagnostics({
        failure_origin: 'token_exchange',
        failure_code: 'setup_token_exchange_failed',
        recommended_actions: [
          { key: 'replace_setup_token', label: 'Replace setup token', severity: 'warning' },
          { key: 'replace_account_and_proxy', label: 'Replace account and proxy', severity: 'danger' },
        ],
      }),
    })

    const action = wrapper.get('[data-testid="action-replaceAccountAndProxy"]')
    expect(action.text()).toContain('重新上号并更换代理')

    await action.trigger('click')
    await flushPromises()

    expect(routerPush).toHaveBeenCalledWith({
      path: '/admin/claude-onboarding',
      query: { source: 'diagnostics-v2' },
    })
  })

  it('keeps swap proxy guarded even if the action handler is invoked with stale empty input', async () => {
    const wrapper = await mountModal({
      account: account({ proxy_id: 7 }),
      diagnostics: diagnostics({
        failure_origin: 'proxy',
        proxy_mismatch: true,
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' }],
      }),
    })

    await wrapper.get('[data-testid="action-swapProxy"]').trigger('click')
    await flushPromises()

    expect(swapProxy).not.toHaveBeenCalled()
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



  it('shows Chinese primary copy for proxy mismatch evidence while keeping diagnostic codes secondary', async () => {
    const wrapper = await mountModal({
      diagnostics: diagnostics({
        failure_origin: 'proxy_mismatch' as FormalPoolOperationsDiagnostics['failure_origin'],
        failure_code: 'bucket_mismatch',
        status_code_bucket: 'rate_limit_5h',
        cc_gateway_seen: true,
        raw_capture_present: true,
        proxy_mismatch: true,
        fallback_detected: true,
        checks: [{ name: 'runtime_evidence_incomplete', status: 'fail', message: 'bucket_mismatch' }],
        recommended_actions: [{ key: 'wait_rate_limit', label: 'wait_rate_limit', severity: 'warning' }],
      }),
    })

    const rootCause = wrapper.get('[data-testid="diagnostics-v2-root-cause"]').text()
    expect(rootCause).toContain('代理出口不一致')
    expect(rootCause).toContain('出口分组不一致')
    expect(rootCause).not.toContain('失败来源：proxy_mismatch')
    expect(rootCause).not.toContain('失败分类：bucket_mismatch')

    await wrapper.get('[data-testid="evidence-toggle"]').trigger('click')
    const originValue = wrapper.get('[data-testid="evidence-item-failure_origin"] div').text()
    const codeValue = wrapper.get('[data-testid="evidence-item-failure_code"] div').text()
    const statusValue = wrapper.get('[data-testid="evidence-item-status_code_bucket"] div').text()
    const checkText = wrapper.get('[data-testid="evidence-group-checks"]').text()
    const actionsText = wrapper.get('[data-testid="evidence-group-actions"]').text()

    expect(originValue).toMatch(/^代理出口不一致/)
    expect(codeValue).toMatch(/^出口分组不一致/)
    expect(statusValue).toMatch(/^5 小时窗口冷却\/限流/)
    expect(checkText).toContain('运行证据不完整')
    expect(actionsText).toContain('等待 5 小时窗口冷却/限流恢复')
    expect(originValue).not.toBe('proxy_mismatch')
    expect(codeValue).not.toBe('bucket_mismatch')
  })


  it('keeps ordinary diagnostic prose as scrubbed text without unknown-code wrappers', async () => {
    const wrapper = await mountModal({
      diagnostics: diagnostics({
        checks: [{ name: 'stage_gate', status: 'fail', message: 'latest healthcheck evidence is required before warming' }],
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy and revalidate', severity: 'warning' }],
      }),
    })

    await wrapper.get('[data-testid="evidence-toggle"]').trigger('click')
    const checksText = wrapper.get('[data-testid="evidence-group-checks"]').text()
    const actionsText = wrapper.get('[data-testid="evidence-group-actions"]').text()

    expect(checksText).toContain('latest healthcheck evidence is required before warming')
    expect(checksText).not.toContain('未知分类')
    expect(actionsText).toContain('Swap proxy and revalidate')
    expect(actionsText).not.toContain('未知动作')
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

  it('requires a different replacement proxy id before executing swap proxy', async () => {
    swapProxy.mockResolvedValue({
      account: account({ status: 'active', schedulable: false, effective_schedulable: false, onboarding_stage: 'runtime_registered' }),
      diagnostics: diagnostics({ onboarding_stage: 'runtime_registered' }),
    })
    const wrapper = await mountModal({
      account: account({ proxy_id: 7 }),
      diagnostics: diagnostics({
        failure_origin: 'proxy',
        proxy_mismatch: true,
        fallback_detected: true,
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy', severity: 'warning' }],
      }),
    })

    const action = wrapper.get('[data-testid="action-swapProxy"]')
    expect(wrapper.get('[data-testid="swap-proxy-id-input"]').exists()).toBe(true)
    expect(action.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('请填写新的出口代理 ID')

    await wrapper.get('[data-testid="swap-proxy-id-input"]').setValue('7')
    expect(action.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('新代理 ID 不能与当前代理相同')

    await wrapper.get('[data-testid="swap-proxy-id-input"]').setValue('12')
    expect(action.attributes('disabled')).toBeUndefined()
    await action.trigger('click')
    await flushPromises()

    expect(swapProxy).toHaveBeenCalledTimes(1)
    expect(swapProxy).toHaveBeenCalledWith(42, {
      proxy_id: 12,
      run_proxy_test: true,
      run_runtime_register: true,
      run_healthcheck: true,
    })
  })


  it('renders and executes manual promote to production for warming accounts with complete evidence', async () => {
    const promotedAccount = account({
      status: 'active',
      schedulable: true,
      effective_schedulable: true,
      onboarding_stage: 'production',
    })
    promoteProduction.mockResolvedValue({
      account: promotedAccount,
      diagnostics: diagnostics({
        onboarding_stage: 'production',
        schedulable: true,
        effective_schedulable: true,
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        recommended_actions: [{ key: 'monitor', label: 'Monitor', severity: 'info' }],
      }),
    })

    const wrapper = await mountModal({
      account: account({ status: 'active', schedulable: true, effective_schedulable: true, onboarding_stage: 'warming' }),
      diagnostics: diagnostics({
        onboarding_stage: 'warming',
        schedulable: true,
        effective_schedulable: true,
        failure_origin: 'unknown',
        failure_code: undefined,
        status_code_bucket: undefined,
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        checks: [
          { name: 'cc_gateway_runtime_registered', status: 'pass', message: 'ok' },
          { name: 'healthcheck_evidence_persisted', status: 'pass', message: 'ok' },
        ],
        recommended_actions: [{ key: 'promote_production', label: 'Promote production', severity: 'info' }],
      }),
    })

    const action = wrapper.get('[data-testid="action-promoteProduction"]')
    expect(action.text()).toContain('进入生产')

    await action.trigger('click')
    await flushPromises()

    expect(promoteProduction).toHaveBeenCalledTimes(1)
    expect(promoteProduction).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ onboarding_stage: 'production' })
  })

  it('renders and executes start warming for healthcheck-passed accounts with complete evidence', async () => {
    const warmingAccount = account({
      status: 'active',
      schedulable: true,
      effective_schedulable: true,
      onboarding_stage: 'warming',
    })
    startWarming.mockResolvedValue({
      // Formal Pool operation responses intentionally return a safe minimal
      // account payload. The modal must merge it with the existing account
      // instead of replacing the full UI context and losing the operator label.
      account: {
        id: warmingAccount.id,
        status: warmingAccount.status,
        schedulable: warmingAccount.schedulable,
        effective_schedulable: warmingAccount.effective_schedulable,
        onboarding_stage: warmingAccount.onboarding_stage,
      },
      diagnostics: diagnostics({
        onboarding_stage: 'warming',
        schedulable: true,
        effective_schedulable: true,
        failure_origin: 'unknown',
        healthcheck_status: 'passed',
        status_code_bucket: 'status_2xx',
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        recommended_actions: [{ key: 'promote_production', label: 'Promote production', severity: 'info' }],
      }),
    })

    const wrapper = await mountModal({
      account: account({ status: 'active', schedulable: false, effective_schedulable: false, onboarding_stage: 'healthcheck_passed' }),
      diagnostics: diagnostics({
        onboarding_stage: 'healthcheck_passed',
        schedulable: false,
        effective_schedulable: false,
        failure_origin: 'local_gate',
        failure_code: undefined,
        healthcheck_status: 'passed',
        status_code_bucket: 'status_2xx',
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        checks: [
          { name: 'cc_gateway_runtime_registered', status: 'pass', message: 'ok' },
          { name: 'healthcheck_evidence_persisted', status: 'pass', message: 'ok' },
        ],
        recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      }),
    })

    expect(wrapper.get('[data-testid="diagnostics-v2-root-cause"]').text()).toContain('健康检查已通过')
    const action = wrapper.get('[data-testid="action-startWarming"]')
    expect(action.text()).toContain('进入预热期')

    await action.trigger('click')
    await flushPromises()

    expect(startWarming).toHaveBeenCalledTimes(1)
    expect(startWarming).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ onboarding_stage: 'warming', effective_schedulable: true })
    expect(wrapper.emitted('updated')?.[0]?.[0]).toMatchObject({ name: 'formal-account', platform: 'anthropic', type: 'oauth' })
    expect(wrapper.get('#diagnostics-v2-title').text()).toContain('formal-account')
  })


  it.each([
    ['proxy mismatch', { failure_origin: 'proxy', proxy_mismatch: true }],
    ['fallback detected', { failure_origin: 'proxy', fallback_detected: true }],
    ['manual risk', { failure_origin: 'upstream', status_code_bucket: 'status_403', risk_text_detected: true }],
    ['rate limit', { failure_origin: 'upstream', failure_code: 'long_context_usage_credits', status_code_bucket: 'status_429', formal_pool_rate_limit_window: '5h' }],
  ] as const)('does not render promoteProduction for warming accounts when %s is present', async (_name, overrides) => {
    const wrapper = await mountModal({
      account: account({ status: 'active', schedulable: true, effective_schedulable: true, onboarding_stage: 'warming' }),
      diagnostics: diagnostics({
        onboarding_stage: 'warming',
        schedulable: true,
        effective_schedulable: true,
        failure_origin: 'unknown',
        failure_code: undefined,
        status_code_bucket: undefined,
        cc_gateway_seen: true,
        cc_gateway_runtime_registered: true,
        cc_gateway_runtime_registered_at: '2026-06-01T00:00:00Z',
        runtime_evidence_complete: true,
        healthcheck_evidence_persisted: true,
        raw_capture_present: true,
        checks: [
          { name: 'cc_gateway_runtime_registered', status: 'pass', message: 'ok' },
          { name: 'healthcheck_evidence_persisted', status: 'pass', message: 'ok' },
        ],
        ...overrides,
        recommended_actions: [{ key: 'promote_production', label: 'Promote production', severity: 'info' }],
      }),
    })

    expect(wrapper.find('[data-testid="action-promoteProduction"]').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('预热完成，可进入生产')
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



  it('does not render raw account id in title and guides OAuth reauth without raw id query params', async () => {
    const rawAccountId = 987654321
    const rawProxyId = 7654321
    const wrapper = await mountModal({
      account: account({ id: rawAccountId, name: '', proxy_id: rawProxyId }),
      diagnostics: diagnostics({ account_id: rawAccountId }),
    })

    expect(wrapper.html()).not.toContain(`#${rawAccountId}`)
    expect(wrapper.text()).not.toContain(`#${rawAccountId}`)
    expect(wrapper.text()).toContain('账号（未命名）')

    await wrapper.get('[data-testid="diagnostics-v2-primary-action"]').trigger('click')
    await flushPromises()

    expect(routerPush).toHaveBeenCalledWith({
      path: '/admin/claude-onboarding',
      query: { source: 'diagnostics-v2' },
    })
    const pushed = JSON.stringify(routerPush.mock.calls)
    expect(pushed).not.toContain(String(rawAccountId))
    expect(pushed).not.toContain(String(rawProxyId))
  })

  it('clicking healthcheck opens ConfirmDialog and does NOT call healthcheck API', async () => {
    const wrapper = await mountModal({ diagnostics: healthcheckScenarioDiagnostics() })
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="action-healthcheck"]').exists()).toBe(true)

    await wrapper.get('[data-testid="action-healthcheck"]').trigger('click')
    await flushPromises()

    const dialog = wrapper.find('[data-testid="healthcheck-confirm-dialog"]')
    expect(dialog.exists()).toBe(true)
    expect(dialog.attributes('data-danger')).toBe('true')
    expect(dialog.attributes('data-message')).toContain('真实上游请求')
    expect(dialog.attributes('data-title')).toContain('定向健康检查')
    expect(dialog.attributes('data-z-index')).toBe('160')
    expect(healthcheck).not.toHaveBeenCalled()
  })

  it('confirming the dialog calls the healthcheck API exactly once', async () => {
    healthcheck.mockResolvedValue({ account: account({ status: 'healthcheck_passed' }) })
    const wrapper = await mountModal({ diagnostics: healthcheckScenarioDiagnostics() })
    await wrapper.get('[data-testid="action-healthcheck"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-testid="confirm-dialog-stub-confirm"]').trigger('click')
    await flushPromises()

    expect(healthcheck).toHaveBeenCalledTimes(1)
    expect(healthcheck).toHaveBeenCalledWith(42)
    // Dialog closes after confirmation.
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(false)
  })

  it('cancelling the dialog closes it and does NOT call the healthcheck API', async () => {
    const wrapper = await mountModal({ diagnostics: healthcheckScenarioDiagnostics() })
    await wrapper.get('[data-testid="action-healthcheck"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(true)

    await wrapper.get('[data-testid="confirm-dialog-stub-cancel"]').trigger('click')
    await flushPromises()

    expect(healthcheck).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="healthcheck-confirm-dialog"]').exists()).toBe(false)
  })

  it('shows ordinary account email in account chrome but hides mixed secret account names', async () => {
    const ordinary = await mountModal({ account: account({ name: 'ops-user@example.com' }) })
    expect(ordinary.get('#diagnostics-v2-title').text()).toContain('ops-user@example.com')
    expect(ordinary.get('[data-testid="diagnostics-v2-command-bar"]').text()).toContain('ops-user@example.com')

    const mixed = await mountModal({ account: account({ name: 'ops-user@example.com sk-ant-secret-token' }) })
    expect(mixed.html()).not.toContain('ops-user@example.com')
    expect(mixed.html()).not.toContain('sk-ant-secret-token')
    expect(mixed.get('#diagnostics-v2-title').text()).toContain('账号（未命名）')
  })

  it('still scrubs ordinary emails from diagnostic evidence text', async () => {
    const wrapper = await mountModal({
      account: account({ name: 'ops-user@example.com' }),
      diagnostics: diagnostics({
        raw_capture_ref: 'evidence for evidence-user@example.com',
        risk_event_ref: 'risk event evidence-user@example.com',
        checks: [{ name: 'stage_gate', status: 'fail', message: 'message evidence-user@example.com' }],
      }),
    })
    await wrapper.get('[data-testid="evidence-toggle"]').trigger('click')

    const evidenceText = wrapper.find('[data-testid="evidence-group-gateway"]').text() + wrapper.find('[data-testid="evidence-group-upstream"]').text() + wrapper.find('[data-testid="evidence-group-checks"]').text()
    expect(evidenceText).not.toContain('evidence-user@example.com')
    expect(evidenceText).toContain('[redacted]')
    expect(wrapper.get('#diagnostics-v2-title').text()).toContain('ops-user@example.com')
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
