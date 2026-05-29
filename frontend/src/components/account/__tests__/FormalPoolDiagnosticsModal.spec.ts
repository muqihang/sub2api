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
    'admin.accounts.formalPoolDiagnostics.actions.startWarming': 'Start warming',
    'admin.accounts.formalPoolDiagnostics.actions.repairToken': 'Repair Token',
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

  it('enables start warming only when recommended or evidence is complete', async () => {
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
      healthcheck_evidence_persisted: false,
    }))
    const allowed = mountModal(baseAccount({ onboarding_stage: 'healthcheck_passed' }))
    await flushPromises()
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
})
