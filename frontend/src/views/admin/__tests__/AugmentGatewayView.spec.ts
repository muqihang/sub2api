import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AugmentGatewayView from '../AugmentGatewayView.vue'

const mockGetSummary = vi.fn()
const mockGetProviderGroups = vi.fn()
const mockGetSourcePriority = vi.fn()
const mockUpdateSourcePriority = vi.fn()
const mockGetModels = vi.fn()
const mockUpdateModel = vi.fn()
const mockListPoolSessions = vi.fn()
const mockCreatePoolBindIntent = vi.fn()
const mockBindPoolSession = vi.fn()
const mockRevokePoolSessionAdmin = vi.fn()
const mockDisablePoolSessionAdmin = vi.fn()
const mockRequireReloginAdmin = vi.fn()
const mockGetDiagnostics = vi.fn()
const mockGetUsage = vi.fn()
const mockShowError = vi.fn()
const mockShowSuccess = vi.fn()
let mockRouteQuery: Record<string, unknown> = {}

vi.mock('vue-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-router')>()
  return {
    ...actual,
    useRoute: () => ({ query: mockRouteQuery }),
  }
})

vi.mock('@/api/admin/augmentGateway', () => ({
  getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
  getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
  getAugmentGatewaySourcePriority: (...args: any[]) => mockGetSourcePriority(...args),
  updateAugmentGatewaySourcePriority: (...args: any[]) => mockUpdateSourcePriority(...args),
  getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
  updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
  listAugmentPoolSessions: (...args: any[]) => mockListPoolSessions(...args),
  createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
  bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
  revokeAugmentPoolSessionAdmin: (...args: any[]) => mockRevokePoolSessionAdmin(...args),
  disableAugmentPoolSessionAdmin: (...args: any[]) => mockDisablePoolSessionAdmin(...args),
  requireAugmentPoolSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
  getAugmentPoolSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
  getAugmentGatewayAdminUsage: (...args: any[]) => mockGetUsage(...args),
  default: {
    getAugmentGatewaySummary: (...args: any[]) => mockGetSummary(...args),
    getAugmentProviderGroups: (...args: any[]) => mockGetProviderGroups(...args),
    getAugmentGatewaySourcePriority: (...args: any[]) => mockGetSourcePriority(...args),
    updateAugmentGatewaySourcePriority: (...args: any[]) => mockUpdateSourcePriority(...args),
    getAugmentGatewayModels: (...args: any[]) => mockGetModels(...args),
    updateAugmentGatewayModel: (...args: any[]) => mockUpdateModel(...args),
    listAugmentPoolSessions: (...args: any[]) => mockListPoolSessions(...args),
    createAugmentPoolSessionBindIntent: (...args: any[]) => mockCreatePoolBindIntent(...args),
    bindAugmentPoolSession: (...args: any[]) => mockBindPoolSession(...args),
    revokeAugmentPoolSessionAdmin: (...args: any[]) => mockRevokePoolSessionAdmin(...args),
    disableAugmentPoolSessionAdmin: (...args: any[]) => mockDisablePoolSessionAdmin(...args),
    requireAugmentPoolSessionReloginAdmin: (...args: any[]) => mockRequireReloginAdmin(...args),
    getAugmentPoolSessionDiagnosticsAdmin: (...args: any[]) => mockGetDiagnostics(...args),
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
    mockRouteQuery = {}
    mockGetSummary.mockResolvedValue({
      provider_groups: [],
      models: [],
      official_session_count: 1,
      active_session_count: 1,
      healthy_session_count: 1,
      source_priority: ['official_quick_login', 'wukong_quick_login'],
    })
    mockGetProviderGroups.mockResolvedValue({
      rows: [
        { provider: 'openai', group_id: 1001, healthy: true, active_accounts: 2, total_accounts: 2, version: 1 },
      ],
    })
    mockGetSourcePriority.mockResolvedValue({
      sources: ['official_quick_login', 'wukong_quick_login'],
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
    mockListPoolSessions.mockResolvedValue({
      rows: [
        {
          id: 42,
          source: 'official_quick_login',
          tenant_origin: 'https://tenant.example.com',
          status: 'active',
          fingerprint_prefix: 'fp-1',
          has_credential_payload: true,
          health_score: 100,
          created_by_admin_id: 7,
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
      id: 42,
      tenant_host: 'tenant.example.com',
      fingerprint_prefix: 'fp-1',
      access_token: 'should-not-render',
    })
  })

  it('renders source priority, provider groups, and pool sessions without secrets', async () => {
    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('openai')
    expect(text).toContain('1001')
    expect(text).toContain('official_quick_login')
    expect(text).toContain('wukong_quick_login')
    expect(text).toContain('tenant.example.com')
    expect(text).not.toContain('access_token')
    expect(text).not.toContain('should-not-render')
  })

  it('updates source priority order', async () => {
    mockUpdateSourcePriority.mockResolvedValue({})

    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="source-priority-down-official_quick_login"]').trigger('click')
    await wrapper.get('[data-test="save-source-priority"]').trigger('click')
    await flushPromises()

    expect(mockUpdateSourcePriority).toHaveBeenCalledWith({
      sources: ['wukong_quick_login', 'official_quick_login'],
    })
    expect(mockShowSuccess).toHaveBeenCalledWith('admin.augmentGateway.saved')
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

  it('calls pool revoke endpoint and refreshes session list', async () => {
    mockRevokePoolSessionAdmin.mockResolvedValue({})

    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="revoke-session-42"]').trigger('click')
    await flushPromises()

    expect(mockRevokePoolSessionAdmin).toHaveBeenCalledWith(42)
    expect(mockListPoolSessions).toHaveBeenCalledTimes(2)
  })

  it('captures callback payload into a pool session using admin pool bind APIs', async () => {
    mockRouteQuery = {
      official_tenant_url: 'https://capture.augment.local',
      official_access_token: 'capture-access-token',
      official_refresh_token: 'capture-refresh-token',
      official_expires_at: '2026-05-08T16:00:00Z',
      official_scopes: 'augment:session',
    }
    mockCreatePoolBindIntent.mockResolvedValue({
      bind_intent_id: 'pool-bind-intent-2',
      state: 'pool-bind-state-2',
      expires_at: '2026-05-08T15:30:00Z',
      bind_token: 'pool-bind-token-2',
    })
    mockBindPoolSession.mockResolvedValue({
      id: 77,
      source: 'official_quick_login',
      tenant_origin: 'https://capture.augment.local',
      status: 'active',
    })

    const wrapper = mount(AugmentGatewayView, {
      global: {
        stubs: { AppLayout: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()

    await wrapper.get('[data-test="capture-pool-session"]').trigger('click')
    await flushPromises()

    expect(mockCreatePoolBindIntent).toHaveBeenCalledWith({
      mode: 'official_passthrough',
      source: 'official_quick_login',
      tenant_allowlist: ['https://capture.augment.local'],
    })
    expect(mockBindPoolSession).toHaveBeenCalledWith({
      bind_token: 'pool-bind-token-2',
      bind_intent_id: 'pool-bind-intent-2',
      state: 'pool-bind-state-2',
      mode: 'official_passthrough',
      source: 'official_quick_login',
      payload: {
        tenant_url: 'https://capture.augment.local',
        access_token: 'capture-access-token',
        refresh_token: 'capture-refresh-token',
        expires_at: '2026-05-08T16:00:00Z',
        scopes: ['augment:session'],
      },
    })
    expect(mockListPoolSessions).toHaveBeenCalledTimes(2)
  })

  it('renders cache hit ratio and cost summary', async () => {
    mockGetSummary.mockResolvedValue({
      provider_groups: [],
      models: [],
      official_session_count: 1,
      active_session_count: 1,
      healthy_session_count: 1,
      cache_hit_ratio: 0.42,
      estimated_cost: 12.3,
      settled_cost: 10.1,
      source_priority: ['official_quick_login', 'wukong_quick_login'],
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
