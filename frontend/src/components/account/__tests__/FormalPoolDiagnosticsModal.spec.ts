import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'

const {
  getDiagnostics,
  replaceSetupToken,
  runtimeRegister,
  healthcheck,
  startWarming,
  promoteProduction,
  swapProxy,
} = vi.hoisted(() => ({
  getDiagnostics: vi.fn(),
  replaceSetupToken: vi.fn(),
  runtimeRegister: vi.fn(),
  healthcheck: vi.fn(),
  startWarming: vi.fn(),
  promoteProduction: vi.fn(),
  swapProxy: vi.fn(),
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
    promoteProduction,
    swapProxy,
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const messages: Record<string, string> = {
    'admin.accounts.formalPool.stage.imported': '已导入，待刷新',
    'admin.accounts.formalPool.stage.refreshed': '已刷新，待运行时注册',
    'admin.accounts.formalPool.stage.runtime_registered': '已完成运行时注册，待健康检查',
    'admin.accounts.formalPool.stage.healthcheck_passed': '健康检查通过，待进入预热',
    'admin.accounts.formalPool.stage.warming': '预热中，低权重可调度',
    'admin.accounts.formalPool.stage.production': '生产中，正常调度',
    'admin.accounts.formalPool.stage.quarantined': '已隔离，需要修复',
    'admin.accounts.formalPool.stage.legacy_unknown': '历史账号，状态未知',
    'admin.accounts.formalPoolDiagnostics.lifecycle': '生命周期',
    'admin.accounts.formalPoolDiagnostics.checkNames.stage_gate': '生命周期阶段门禁',
    'admin.accounts.formalPoolDiagnostics.checkNames.cc_gateway_runtime_registered': 'CC Gateway 运行时注册',
    'admin.accounts.formalPoolDiagnostics.checkNames.healthcheck_evidence_persisted': '健康检查证据已持久化',
    'admin.accounts.formalPoolDiagnostics.checkNames.not_formal_pool': '正式号池账号类型',
    'admin.accounts.formalPoolDiagnostics.checkMessages.account_not_found': '账号不存在',
    'admin.accounts.formalPoolDiagnostics.checkMessages.account_is_not_a_formal_pool_anthropic_oauth_setup_token_account': '账号不是正式号池 Anthropic OAuth/Setup Token 账号',
    'admin.accounts.formalPoolDiagnostics.checkMessages.cc_gateway_runtime_identity_bucket_mapping_must_be_registered_before_warming': '进入预热前必须完成 CC Gateway 运行时身份和桶映射注册',
    'admin.accounts.formalPoolDiagnostics.checkMessages.cc_gateway_runtime_identity_bucket_mapping_must_include_registration_timestamp_before_warming': '进入预热前 CC Gateway 运行时注册证据必须包含注册时间',
    'admin.accounts.formalPoolDiagnostics.checkMessages.latest_healthcheck_evidence_is_required_before_warming': '进入预热前必须保留最新健康检查证据',
    'admin.accounts.formalPoolDiagnostics.checkMessages.quarantined_accounts_cannot_be_scheduled': '已隔离账号不能调度',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.upstream': 'Upstream',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.local_gate': 'Local gate',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.cc_gateway_control_plane': 'CC Gateway control plane',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.proxy': 'Proxy',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.token_exchange': 'Token exchange',
    'admin.accounts.formalPoolDiagnostics.actions.startWarming': '进入预热',
    'admin.accounts.formalPoolDiagnostics.actions.promoteProduction': '进入生产',
    'admin.accounts.formalPoolDiagnostics.actions.repairToken': '替换 Setup Token 登录态',
    'admin.accounts.formalPoolDiagnostics.actions.runtimeRegister': '运行时注册/映射',
    'admin.accounts.formalPoolDiagnostics.actions.healthcheck': '定向健康检查',
    'admin.accounts.formalPoolDiagnostics.actions.proxySwap': '更换出口代理',
    'admin.accounts.formalPoolDiagnostics.noRawTokenWarning': 'Secrets are scrubbed.',
    'admin.accounts.formalPoolDiagnostics.noRawTokenWarningSetupToken': 'ST tokens are scrubbed.',
    'admin.accounts.formalPoolDiagnostics.startWarmingBlockedRuntime': 'Blocked until runtime registered',
    'admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeRegisteredAt': 'Runtime registered at',
    'admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeEvidenceComplete': '运行时证据完整',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.refresh_only': '刷新登录凭证',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.runtime_register': '运行时注册/映射',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.healthcheck': '定向健康检查',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.start_warming': '进入预热',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.promote_production': '进入生产',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.replace_setup_token': '替换 Setup Token 登录态',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.reauthorize_oauth': '重新 OAuth 授权',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.monitor': '无需操作，继续观测',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.quarantine': '隔离账号',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.swap_proxy': '更换出口代理',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.repair_token': '替换 Setup Token 登录态',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.repair_oauth': '重新 OAuth 授权',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.title': 'OAuth 恢复序列',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.body': '请重新 OAuth 授权，然后回到本弹窗继续运行时注册、定向健康检查和预热。',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRefresh': '1. 重新 OAuth 授权。',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRuntime': '2. 运行时注册/映射。',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepHealthcheck': '3. 定向健康检查会发起一次极小真实上游请求，点击前必须确认。',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepWarming': '4. 证据完整后进入预热。',
    'admin.accounts.formalPoolDiagnostics.directedHealthcheckWarning': '定向健康检查会发起一次极小真实上游请求；必须管理员确认后才点击。',
    'admin.accounts.formalPoolDiagnostics.directedHealthcheckConfirm': '确认继续？此操作会发起一次极小真实上游请求。',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.local_gate': 'Local lifecycle gate blocked scheduling. Click runtime register first if runtime evidence is missing.',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.cc_gateway_control_plane': 'CC Gateway has not confirmed runtime registration. Click runtime register, then directed healthcheck.',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.upstream': 'The upstream provider rejected the directed check. Repair credentials or reauthorize, then directed healthcheck.',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.proxy': 'Proxy evidence is invalid. Swap proxy and revalidate first.',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.token_exchange': 'Credential exchange failed. Setup-token accounts can repair ST token; OAuth accounts should refresh or reauthorize first.',
    'admin.accounts.formalPoolDiagnostics.failureOriginDescriptions.unknown': 'Refresh diagnostics, then follow the latest recommended action.',
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, fallback?: string) => {
        if (Object.prototype.hasOwnProperty.call(messages, key)) return messages[key]
        if (fallback !== undefined && fallback !== '') return fallback
        return key
      },
    }),
  }
})

import { FormalPoolOperationError } from '@/api/admin/formalPoolOperations'
import FormalPoolDiagnosticsModal from '@/components/account/FormalPoolDiagnosticsModal.vue'

const baseAccount = (overrides: Partial<Account> = {}): Account => ({
  id: 5,
  name: 'formal-account',
  platform: 'anthropic',
  type: 'setup-token',
  credentials: {},
  proxy_id: 7,
  concurrency: 1,
  priority: 0,
  status: 'error',
  error_message: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-05-29T00:00:00Z',
  updated_at: '2026-05-29T00:00:00Z',
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
})

const diagnostics = (overrides: Partial<FormalPoolOperationsDiagnostics> = {}): FormalPoolOperationsDiagnostics => ({
  account_id: 5,
  is_formal_pool: true,
  onboarding_stage: 'quarantined',
  schedulable: false,
  effective_schedulable: false,
  failure_origin: 'upstream',
  checks: [{ name: 'stage_gate', status: 'fail', message: 'quarantined accounts cannot be scheduled' }],
  recommended_actions: [{ key: 'replace_setup_token', label: 'Replace setup token', severity: 'warning' }],
  ...overrides,
})

const mountModal = (account: Account = baseAccount()) => mount(FormalPoolDiagnosticsModal, {
  props: { show: true, account },
  global: {
    stubs: {
      BaseDialog: {
        props: ['show', 'title'],
        template: '<section v-if="show"><h1>{{ title }}</h1><slot /><slot name="footer" /></section>',
      },
      Icon: true,
    },
  },
})

describe('FormalPoolDiagnosticsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getDiagnostics.mockResolvedValue(diagnostics())
  })

  it('shows failure origin labels and checks', async () => {
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('Upstream')
    expect(wrapper.text()).toContain('生命周期阶段门禁')
    expect(wrapper.text()).toContain('已隔离账号不能调度')
    expect(wrapper.text()).not.toContain('stage_gate')
    expect(wrapper.text()).not.toContain('quarantined accounts cannot be scheduled')
  })

  it('scrubs raw setup-token secrets from rendered operation errors', async () => {
    replaceSetupToken.mockRejectedValue(new Error(
      'failed for sk-ant-sid-test-secret with Bearer abcdefghijklmn session_key=secret-value ' +
      'access_token=access-secret refresh_token: refresh-secret Authorization: Bearer authorization-secret password=password-secret ' +
      'proxy_url=http://user:pass@host:8080 proxy=http://user:pass@host:8080 raw=raw-secret raw_cookie=raw-cookie-secret'
    ))
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.get('[data-test="session-key-input"]').setValue('sk-ant-sid-test-secret')
    await wrapper.get('[data-test="repair-token-button"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-test="session-key-input"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).not.toContain('sk-ant-sid-test-secret')
    expect(wrapper.text()).not.toContain('abcdefghijklmn')
    expect(wrapper.text()).not.toContain('secret-value')
    expect(wrapper.text()).not.toContain('access-secret')
    expect(wrapper.text()).not.toContain('refresh-secret')
    expect(wrapper.text()).not.toContain('authorization-secret')
    expect(wrapper.text()).not.toContain('password-secret')
    expect(wrapper.text()).not.toContain('user:pass@host')
    expect(wrapper.text()).not.toContain('raw-secret')
    expect(wrapper.text()).not.toContain('raw-cookie-secret')
    expect(wrapper.text()).toContain('[redacted]')
  })

  it('enables start warming only when recommended and full evidence is complete', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'healthcheck', label: 'Run directed healthcheck', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: false,
    }))
    const blocked = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()
    expect(blocked.get('[data-test="start-warming-button"]').attributes('disabled')).toBeDefined()

    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: true,
      status_code_bucket: 'status_2xx',
      cc_gateway_seen: true,
      raw_capture_present: true,
      fallback_detected: false,
      proxy_mismatch: false,
      risk_text_detected: false,
      cc_gateway_runtime_registered: true,
      cc_gateway_runtime_registered_at: '2026-05-30T01:02:03Z',
      runtime_evidence_complete: true,
    }))
    const allowed = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()
    expect(allowed.get('[data-test="start-warming-button"]').attributes('disabled')).toBeUndefined()
  })

  it('does not enable start warming without runtime registered evidence even when recommended', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: true,
      status_code_bucket: 'status_2xx',
      cc_gateway_seen: true,
      raw_capture_present: true,
      fallback_detected: false,
      proxy_mismatch: false,
      risk_text_detected: false,
      cc_gateway_runtime_registered: false,
    }))
    const wrapper = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()

    const startButton = wrapper.get('[data-test="start-warming-button"]')
    expect(startButton.attributes('disabled')).toBeDefined()
    expect(startButton.attributes('title')).toContain('runtime')
    expect(wrapper.text()).not.toContain('sk-ant-sid')
  })

  it('enables start warming with runtime registered and full healthcheck evidence', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: true,
      status_code_bucket: 'status_2xx',
      cc_gateway_seen: true,
      raw_capture_present: true,
      fallback_detected: false,
      proxy_mismatch: false,
      risk_text_detected: false,
      cc_gateway_runtime_registered: true,
      cc_gateway_runtime_registered_at: '2026-05-30T01:02:03Z',
      runtime_evidence_complete: true,
    }))
    const wrapper = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()

    expect(wrapper.get('[data-test="start-warming-button"]').attributes('disabled')).toBeUndefined()
  })


  it('shows runtime registration timestamp and blocks warming when timestamp is missing', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: true,
      status_code_bucket: 'status_2xx',
      cc_gateway_seen: true,
      raw_capture_present: true,
      fallback_detected: false,
      proxy_mismatch: false,
      risk_text_detected: false,
      cc_gateway_runtime_registered: true,
      cc_gateway_runtime_registered_at: '',
      runtime_evidence_complete: false,
    }))
    const blocked = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()
    expect(blocked.get('[data-test="start-warming-button"]').attributes('disabled')).toBeDefined()
    expect(blocked.text()).toContain('运行时证据完整')

    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
      onboarding_stage: 'healthcheck_passed',
      healthcheck_evidence_persisted: true,
      status_code_bucket: 'status_2xx',
      cc_gateway_seen: true,
      raw_capture_present: true,
      fallback_detected: false,
      proxy_mismatch: false,
      risk_text_detected: false,
      cc_gateway_runtime_registered: true,
      cc_gateway_runtime_registered_at: '2026-05-30T01:02:03Z',
      runtime_evidence_complete: true,
    }))
    const allowed = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()
    expect(allowed.text()).toContain('2026-05-30T01:02:03Z')
    expect(allowed.get('[data-test="start-warming-button"]').attributes('disabled')).toBeUndefined()
  })

  it('shows repair-token form only for Anthropic setup-token formal-pool accounts', async () => {
    const setupToken = mountModal()
    await flushPromises()
    expect(setupToken.find('[data-test="session-key-input"]').exists()).toBe(true)

    const oauth = mountModal(baseAccount({ type: 'oauth' }))
    await flushPromises()
    expect(oauth.find('[data-test="session-key-input"]').exists()).toBe(false)
  })

  it('keeps failed operation diagnostics visible when the API returns FormalPoolOperationError', async () => {
    replaceSetupToken.mockRejectedValue(new FormalPoolOperationError('setup-token credential exchange failed', {
      status: 400,
      account: { id: 5, status: 'error', schedulable: false, onboarding_stage: 'quarantined' },
      diagnostics: diagnostics({
        failure_origin: 'token_exchange',
        recommended_actions: [
          { key: 'replace_account_and_proxy', label: 'Replace account and proxy', severity: 'danger' },
        ],
      }),
    }))
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.get('[data-test="session-key-input"]').setValue('sk-ant-sid-test-secret')
    await wrapper.get('[data-test="repair-token-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Replace account and proxy')
    expect(wrapper.text()).toContain('setup-token credential exchange failed')
    expect(wrapper.text()).not.toContain('sk-ant-sid-test-secret')
  })

  it('shows ST-token wording for setup-token repair recommendations', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      recommended_actions: [{ key: 'replace_setup_token', label: 'Replace setup token', severity: 'warning' }],
    }))
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('替换 Setup Token 登录态')
    expect(wrapper.find('[data-test="session-key-input"]').exists()).toBe(true)
  })

  it('shows OAuth recovery guidance instead of an ST-token input for OAuth formal-pool accounts', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      failure_origin: 'token_exchange',
      recommended_actions: [{ key: 'replace_setup_token', label: 'Replace setup token', severity: 'warning' }],
    }))
    const wrapper = mountModal(baseAccount({ type: 'oauth' }))
    await flushPromises()

    expect(wrapper.find('[data-test="session-key-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('OAuth 恢复序列')
    expect(wrapper.text()).toContain('重新 OAuth 授权')
    expect(wrapper.text()).not.toContain('Repair ST token')
    expect(wrapper.text()).toContain('重新 OAuth 授权')
    expect(wrapper.text()).toContain('运行时注册/映射')
    expect(wrapper.text()).toContain('会发起一次极小真实上游请求')
  })

  it('explains failure origin and next action for each Formal Pool failure origin', async () => {
    const origins = [
      ['local_gate', 'Local lifecycle gate blocked scheduling'],
      ['cc_gateway_control_plane', 'CC Gateway has not confirmed runtime registration'],
      ['upstream', 'The upstream provider rejected the directed check'],
      ['proxy', 'Proxy evidence is invalid'],
      ['token_exchange', 'Credential exchange failed'],
    ] as const

    for (const [origin, copy] of origins) {
      getDiagnostics.mockResolvedValueOnce(diagnostics({ failure_origin: origin }))
      const wrapper = mountModal()
      await flushPromises()
      expect(wrapper.text()).toContain(copy)
      wrapper.unmount()
    }
  })

  it('refreshes diagnostics after operation failure when the error does not include diagnostics', async () => {
    getDiagnostics
      .mockResolvedValueOnce(diagnostics({ failure_origin: 'local_gate' }))
      .mockResolvedValueOnce(diagnostics({
        failure_origin: 'proxy',
        recommended_actions: [{ key: 'swap_proxy', label: 'Swap proxy and revalidate', severity: 'warning' }],
      }))
    runtimeRegister.mockRejectedValueOnce(new FormalPoolOperationError('runtime failed'))
    const wrapper = mountModal()
    await flushPromises()

    const runtimeButton = wrapper.findAll('button').find(button => button.text().includes('运行时注册/映射'))
    expect(runtimeButton).toBeTruthy()
    await runtimeButton!.trigger('click')
    await flushPromises()

    expect(getDiagnostics).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('更换出口代理')
  })

  it('calls runtime register, directed healthcheck, and proxy swap with account-level payloads from modal actions', async () => {
    getDiagnostics.mockResolvedValue(diagnostics())
    runtimeRegister.mockResolvedValue({ account: baseAccount({ onboarding_stage: 'runtime_registered' }) })
    healthcheck.mockResolvedValue({ account: baseAccount({ onboarding_stage: 'healthcheck_passed' }) })
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
    swapProxy.mockResolvedValue({ account: baseAccount({ proxy_id: 9 }) })
    const wrapper = mountModal()
    await flushPromises()

    const findButton = (text: string) => wrapper.findAll('button').find(button => button.text().includes(text))!

    await findButton('运行时注册/映射').trigger('click')
    await flushPromises()
    expect(runtimeRegister).toHaveBeenCalledWith(5)

    await findButton('定向健康检查').trigger('click')
    await flushPromises()
    expect(healthcheck).toHaveBeenCalledWith(5)

    await wrapper.get('[data-test="proxy-id-input"]').setValue('9')
    await findButton('更换出口代理').trigger('click')
    await flushPromises()
    expect(swapProxy).toHaveBeenCalledWith(5, {
      proxy_id: 9,
      run_proxy_test: true,
      run_runtime_register: true,
      run_healthcheck: true,
    })
    confirmSpy.mockRestore()
  })


  it('renders exact Chinese lifecycle labels and directed healthcheck confirmation copy', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      onboarding_stage: 'runtime_registered',
      recommended_actions: [{ key: 'healthcheck', label: 'Run directed healthcheck', severity: 'info' }],
    }))
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValueOnce(false)
    const wrapper = mountModal(baseAccount({ onboarding_stage: 'runtime_registered' }))
    await flushPromises()

    expect(wrapper.get('[data-test="formal-pool-stepper"]').text()).toContain('已导入，待刷新')
    expect(wrapper.text()).toContain('已完成运行时注册，待健康检查')
    expect(wrapper.text()).toContain('定向健康检查')
    expect(wrapper.text()).toContain('会发起一次极小真实上游请求')

    await wrapper.get('[data-test="directed-healthcheck-button"]').trigger('click')
    await flushPromises()
    expect(confirmSpy).toHaveBeenCalledWith(expect.stringContaining('会发起一次极小真实上游请求'))
    expect(healthcheck).not.toHaveBeenCalled()
    confirmSpy.mockRestore()
  })

  it('does not show primary repair or directed healthcheck controls for healthy production monitor action', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      onboarding_stage: 'production',
      failure_origin: 'unknown',
      schedulable: true,
      effective_schedulable: true,
      recommended_actions: [{ key: 'monitor', label: 'Monitor only', severity: 'info' }],
    }))
    const wrapper = mountModal(baseAccount({ onboarding_stage: 'production', status: 'active', schedulable: true, effective_schedulable: true }))
    await flushPromises()

    expect(wrapper.text()).toContain('生产中，正常调度')
    expect(wrapper.text()).toContain('无需操作，继续观测')
    expect(wrapper.find('[data-test="repair-token-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="directed-healthcheck-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="promote-production-button"]').exists()).toBe(false)
  })

  it('shows setup-token invalid_grant as replacing Setup Token login state', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      failure_code: 'invalid_grant',
      recommended_actions: [{ key: 'replace_setup_token', label: 'Replace setup token', severity: 'danger' }],
    }))
    const wrapper = mountModal(baseAccount({ type: 'setup-token' }))
    await flushPromises()

    expect(wrapper.text()).toContain('替换 Setup Token 登录态')
    expect(wrapper.find('[data-test="repair-token-button"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Replace setup token')
  })

  it('shows OAuth invalid_grant as OAuth reauthorization guidance', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      failure_code: 'invalid_grant',
      failure_origin: 'token_exchange',
      recommended_actions: [{ key: 'reauthorize_oauth', label: 'Reauthorize OAuth', severity: 'danger' }],
    }))
    const wrapper = mountModal(baseAccount({ type: 'oauth' }))
    await flushPromises()

    expect(wrapper.find('[data-test="session-key-input"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="oauth-reauthorize-guidance"]').text()).toContain('重新 OAuth 授权')
    expect(wrapper.text()).not.toContain('Reauthorize OAuth')
  })

  it('promotes warming accounts to production from the recommended action', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      onboarding_stage: 'warming',
      recommended_actions: [{ key: 'promote_production', label: 'Promote production', severity: 'info' }],
    }))
    promoteProduction.mockResolvedValue({ account: baseAccount({ onboarding_stage: 'production' }) })
    const wrapper = mountModal(baseAccount({ onboarding_stage: 'warming' }))
    await flushPromises()

    await wrapper.get('[data-test="promote-production-button"]').trigger('click')
    await flushPromises()
    expect(promoteProduction).toHaveBeenCalledWith(5)
  })



  it('localizes known backend check names and messages instead of rendering machine English', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      checks: [
        { name: 'cc_gateway_runtime_registered', status: 'fail', message: 'cc gateway runtime identity/bucket mapping must be registered before warming' },
        { name: 'healthcheck_evidence_persisted', status: 'warn', message: 'latest healthcheck evidence is required before warming' },
      ],
      recommended_actions: [{ key: 'start_warming', label: 'Start warming', severity: 'info' }],
    }))
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('CC Gateway 运行时注册')
    expect(wrapper.text()).toContain('进入预热前必须完成 CC Gateway 运行时身份和桶映射注册')
    expect(wrapper.text()).toContain('健康检查证据已持久化')
    expect(wrapper.text()).toContain('进入预热前必须保留最新健康检查证据')
    expect(wrapper.text()).not.toContain('cc_gateway_runtime_registered')
    expect(wrapper.text()).not.toContain('latest healthcheck evidence is required before warming')
  })

  it('scrubs unsafe diagnostics labels, check messages, and evidence values before rendering', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      checks: [{ name: 'stage_gate sk-ant-sid-check-secret', status: 'fail', message: 'raw_prompt=user@example.com access_token=secret-token proxy=http://user:pass@host:8080' }],
      raw_capture_ref: 'hmac-sha256:' + 'a'.repeat(64),
      risk_event_ref: 'admin@example.com sk-ant-sid-risk-secret',
      recommended_actions: [{ key: 'unknown_action', label: 'raw_body access_token=secret-label sk-ant-sid-label-secret', severity: 'warning' }],
    }))
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('[redacted]')
    expect(wrapper.text()).toContain('hmac-sha256:' + 'a'.repeat(64))
    expect(wrapper.text()).not.toContain('sk-ant-sid-check-secret')
    expect(wrapper.text()).not.toContain('user@example.com')
    expect(wrapper.text()).not.toContain('secret-token')
    expect(wrapper.text()).not.toContain('user:pass@host')
    expect(wrapper.text()).not.toContain('sk-ant-sid-risk-secret')
    expect(wrapper.text()).not.toContain('secret-label')
    expect(wrapper.text()).not.toContain('sk-ant-sid-label-secret')
  })
})
