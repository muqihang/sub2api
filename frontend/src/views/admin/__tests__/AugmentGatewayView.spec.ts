import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AugmentGatewayView from '../AugmentGatewayView.vue'

const mockGetSummary = vi.fn()
const mockGetProviderGroups = vi.fn()
const mockUpdateProviderGroups = vi.fn()
const mockGetModels = vi.fn()
const mockUpdateModel = vi.fn()
const mockListOfficialSessions = vi.fn()
const mockRevokeOfficialSessionAdmin = vi.fn()
const mockDisableOfficialSessionAdmin = vi.fn()
const mockRequireReloginAdmin = vi.fn()
const mockGetDiagnostics = vi.fn()
const mockGetUsage = vi.fn()
const mockShowError = vi.fn()
const mockShowSuccess = vi.fn()

vi.mock('@/api/admin/augmentGateway', () => ({
  getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
  getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
  updateAugmentProviderGroups: (...args: any[]) => mockUpdateProviderGroups(...args),
  getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
  updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
  listAugmentOfficialSessions: (...args: any[]) => mockListOfficialSessions(...args),
  revokeAugmentOfficialSessionAdmin: (...args: any[]) => mockRevokeOfficialSessionAdmin(...args),
  disableAugmentOfficialSessionAdmin: (...args: any[]) => mockDisableOfficialSessionAdmin(...args),
  requireAugmentOfficialSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
  getAugmentOfficialSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
  getAugmentGatewayAdminUsage: (...args: any[]) => mockGetUsage(...args),
  default: {
    getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
    getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
    updateAugmentProviderGroups: (...args: any[]) => mockUpdateProviderGroups(...args),
    getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
    updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
    listAugmentOfficialSessions: (...args: any[]) => mockListOfficialSessions(...args),
    revokeAugmentOfficialSessionAdmin: (...args: any[]) => mockRevokeOfficialSessionAdmin(...args),
    disableAugmentOfficialSessionAdmin: (...args: any[]) => mockDisableOfficialSessionAdmin(...args),
    requireAugmentOfficialSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
    getAugmentOfficialSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
    getAugmentGatewayAdminUsage: (...args: any[]) => mockGetUsage(...args),
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: (...args: any[]) => mockShowError(...args),
    showSuccess: (...args: any[]) => mockShowSuccess(...args),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

describe('AugmentGateway admin', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetSummary.mockResolvedValue({
      provider_groups: [],
      models: [],
      official_session_count: 1,
    })
    mockGetProviderGroups.mockResolvedValue({
      rows: [
        { provider: 'openai', group_id: 1001, healthy: true, active_accounts: 2, total_accounts: 2, version: 1 },
      ],
    })
    mockGetModels.mockResolvedValue({
      rows: [
        {
          model: { id: 'gpt-5.4', provider: 'openai', upstream_model: 'gpt-5.4' },
          enabled: true,
          visible: true,
          smoke_status: 'passed',
          provider_healthy: true,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
        {
          model: { id: 'claude-sonnet-4-5', provider: 'anthropic', upstream_model: 'claude-sonnet-4-5' },
          enabled: false,
          visible: false,
          smoke_status: 'pending',
          provider_healthy: false,
          settings_version: 4,
          settings_namespace: 'gateway.augment.enabled_models',
        },
      ],
    })
    mockListOfficialSessions.mockResolvedValue({
      rows: [
        {
          user_id: 42,
          source: 'official_quick_login',
          tenant_origin: 'https://tenant.example.com',
          status: 'active',
          fingerprint_prefix: 'fp-1',
          has_credential_payload: true,
          access_token: 'should-not-render',
        },
      ],
    })
    mockGetUsage.mockResolvedValue({
      rows: [
        { model: 'gpt-5.4', request_id: 'req-1', estimated_cost: 1.2, settled_cost: 1.1, cache_read_tokens: 10, cache_creation_tokens: 5 },
      ],
      page: { page: 1, page_size: 20, pages: 1, total: 1 },
    })
    mockGetDiagnostics.mockResolvedValue({
      user_id: 42,
      tenant_host: 'tenant.example.com',
      fingerprint_prefix: 'fp-1',
      access_token: 'should-not-render',
    })
  })

  it('renders provider group bindings', async () => {
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()
    expect(wrapper.text()).toContain('openai')
    expect(wrapper.text()).toContain('1001')
  })

  it('prevents model visible toggle when smoke status is not ok', async () => {
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const blocked = wrapper.get('[data-test="model-toggle-claude-sonnet-4-5"]')
    expect(blocked.attributes('disabled')).toBeDefined()
  })

  it('renders official sessions without secrets', async () => {
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('tenant.example.com')
    expect(text).not.toContain('access_token')
    expect(text).not.toContain('should-not-render')
  })

  it('calls revoke endpoint and refreshes session list', async () => {
    mockRevokeOfficialSessionAdmin.mockResolvedValue({})
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="revoke-session-42"]').trigger('click')
    await flushPromises()

    expect(mockRevokeOfficialSessionAdmin).toHaveBeenCalledWith(42)
    expect(mockListOfficialSessions).toHaveBeenCalledTimes(2)
  })

  it('renders cache hit ratio and cost summary', async () => {
    mockGetSummary.mockResolvedValue({
      provider_groups: [],
      models: [],
      official_session_count: 1,
      cache_hit_ratio: 0.42,
      estimated_cost: 12.3,
      settled_cost: 10.1,
    })
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('0.42')
    expect(text).toContain('12.3')
    expect(text).toContain('10.1')
  })
})

describe('AugmentGateway admin route', () => {
  it('requires admin route meta', async () => {
    const { routes } = await import('@/router')
    const route = routes.find((item) => item.path === '/admin/augment-gateway')
    expect(route?.meta?.requiresAdmin).toBe(true)
  })
})
