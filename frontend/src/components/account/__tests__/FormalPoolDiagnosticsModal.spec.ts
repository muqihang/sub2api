import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { Account, FormalPoolOperationsDiagnostics } from '@/types'

const {
  getDiagnostics,
  replaceSetupToken,
  runtimeRegister,
  healthcheck,
  startWarming,
  swapProxy,
} = vi.hoisted(() => ({
  getDiagnostics: vi.fn(),
  replaceSetupToken: vi.fn(),
  runtimeRegister: vi.fn(),
  healthcheck: vi.fn(),
  startWarming: vi.fn(),
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
    'admin.accounts.formalPoolDiagnostics.failureOrigins.upstream': 'Upstream',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.local_gate': 'Local gate',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.cc_gateway_control_plane': 'CC Gateway control plane',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.proxy': 'Proxy',
    'admin.accounts.formalPoolDiagnostics.failureOrigins.token_exchange': 'Token exchange',
    'admin.accounts.formalPoolDiagnostics.actions.startWarming': 'Start warming',
    'admin.accounts.formalPoolDiagnostics.actions.repairToken': 'Repair ST token',
    'admin.accounts.formalPoolDiagnostics.actions.runtimeRegister': 'Runtime register',
    'admin.accounts.formalPoolDiagnostics.actions.healthcheck': 'Directed healthcheck',
    'admin.accounts.formalPoolDiagnostics.actions.proxySwap': 'Swap proxy and revalidate',
    'admin.accounts.formalPoolDiagnostics.noRawTokenWarning': 'Secrets are scrubbed.',
    'admin.accounts.formalPoolDiagnostics.noRawTokenWarningSetupToken': 'ST tokens are scrubbed.',
    'admin.accounts.formalPoolDiagnostics.startWarmingBlockedRuntime': 'Blocked until runtime registered',
    'admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeRegisteredAt': 'Runtime registered at',
    'admin.accounts.formalPoolDiagnostics.evidenceLabels.runtimeEvidenceComplete': 'Runtime evidence complete',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.repair_token': 'Repair ST token',
    'admin.accounts.formalPoolDiagnostics.recommendedActionKeys.repair_oauth': 'Refresh-only or reauthorize OAuth',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.title': 'OAuth recovery sequence',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.body': 'Use the account menu to refresh-only or reauthorize first, then run runtime register, directed healthcheck, and warming here.',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRefresh': '1. Refresh-only or reauthorize from the global account menu.',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepRuntime': '2. Runtime register.',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepHealthcheck': '3. Directed healthcheck sends one tiny real messages request; confirm before clicking.',
    'admin.accounts.formalPoolDiagnostics.oauthRecovery.stepWarming': '4. Start warming after evidence is complete.',
    'admin.accounts.formalPoolDiagnostics.directedHealthcheckWarning': 'Directed healthcheck sends one tiny real messages request; click only after admin confirmation.',
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
      t: (key: string, fallback?: string) => messages[key] ?? fallback ?? key,
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
  recommended_actions: [{ key: 'repair_token', label: 'Repair token first', severity: 'warning' }],
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
    expect(wrapper.text()).toContain('stage_gate')
    expect(wrapper.text()).toContain('quarantined accounts cannot be scheduled')
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
    expect(blocked.text()).toContain('Runtime evidence complete')

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
      recommended_actions: [{ key: 'repair_token', label: 'Repair token first', severity: 'warning' }],
    }))
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.text()).toContain('Repair ST token')
    expect(wrapper.find('[data-test="session-key-input"]').exists()).toBe(true)
  })

  it('shows OAuth recovery guidance instead of an ST-token input for OAuth formal-pool accounts', async () => {
    getDiagnostics.mockResolvedValueOnce(diagnostics({
      failure_origin: 'token_exchange',
      recommended_actions: [{ key: 'repair_token', label: 'Repair token first', severity: 'warning' }],
    }))
    const wrapper = mountModal(baseAccount({ type: 'oauth' }))
    await flushPromises()

    expect(wrapper.find('[data-test="session-key-input"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('OAuth recovery sequence')
    expect(wrapper.text()).toContain('Refresh-only or reauthorize OAuth')
    expect(wrapper.text()).not.toContain('Repair ST token')
    expect(wrapper.text()).toContain('refresh-only or reauthorize')
    expect(wrapper.text()).toContain('Runtime register')
    expect(wrapper.text()).toContain('Directed healthcheck sends one tiny real messages request')
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

    const runtimeButton = wrapper.findAll('button').find(button => button.text().includes('Runtime register'))
    expect(runtimeButton).toBeTruthy()
    await runtimeButton!.trigger('click')
    await flushPromises()

    expect(getDiagnostics).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('Swap proxy and revalidate')
  })

  it('calls runtime register, directed healthcheck, and proxy swap with account-level payloads from modal actions', async () => {
    getDiagnostics.mockResolvedValue(diagnostics())
    runtimeRegister.mockResolvedValue({ account: baseAccount({ onboarding_stage: 'runtime_registered' }) })
    healthcheck.mockResolvedValue({ account: baseAccount({ onboarding_stage: 'healthcheck_passed' }) })
    swapProxy.mockResolvedValue({ account: baseAccount({ proxy_id: 9 }) })
    const wrapper = mountModal()
    await flushPromises()

    const findButton = (text: string) => wrapper.findAll('button').find(button => button.text().includes(text))!

    await findButton('Runtime register').trigger('click')
    await flushPromises()
    expect(runtimeRegister).toHaveBeenCalledWith(5)

    await findButton('Directed healthcheck').trigger('click')
    await flushPromises()
    expect(healthcheck).toHaveBeenCalledWith(5)

    await wrapper.get('[data-test="proxy-id-input"]').setValue('9')
    await findButton('Swap proxy and revalidate').trigger('click')
    await flushPromises()
    expect(swapProxy).toHaveBeenCalledWith(5, {
      proxy_id: 9,
      run_proxy_test: true,
      run_runtime_register: true,
      run_healthcheck: true,
    })
  })
})
